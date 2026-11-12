package reconciliation

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/settings"
	"github.com/jeffhou369/wurzburg/backend/internal/subnets"
)

// sqlChunkSize 是 IN (...) 子句一次最多帶入的參數個數，避免單一語句過長。
const sqlChunkSize = 500

// Service 實作將一輪 ARP 觀測整併進 IP 紀錄、多來源在線／離線判定與 MAC 衝突偵測（Ticket 13 & 25）。
//
// 一輪整併分兩階段（開發規格 3.4、3.6）：
//  1. 逐 VRF 一筆交易：先以 Resolver.LockSnapshot 鎖定範圍並讀取當下資格，再建立／更新 IP 紀錄與 ARP 觀測，
//     最後累計「本 VRF 原本看得到、本輪未看見」的未見輪數並移除超過寬限者；
//  2. 所有 VRF 與寬限都處理完之後，才對受影響的 IP 逐筆決策（在線／離線、MAC 採信、衝突開立與自動解消）。
//
// 逐 IP 的決策只看「集合」（該 IP 目前所有觀測的不重複 MAC），不依賴 outputs 的順序。
type Service struct {
	db        *sql.DB
	clock     clock.Clock
	settings  *settings.Store
	resolver  *subnets.Resolver
	ouiLookup interfaces.OUILookup
}

func NewService(
	db *sql.DB,
	clk clock.Clock,
	sett *settings.Store,
	res *subnets.Resolver,
	oui interfaces.OUILookup,
) *Service {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &Service{
		db:        db,
		clock:     clk,
		settings:  sett,
		resolver:  res,
		ouiLookup: oui,
	}
}

// observation 是單一 VRF 對單一 IPv4 位址在本輪採用的一筆 ARP 觀測。
type observation struct {
	ip      string
	ipNum   uint32
	mac     string
	ifIndex int64
	ifName  string
	arpType string
}

// vrfWork 是單一 VRF 讀取設定在本輪的待處理資料。
type vrfWork struct {
	vrfConfigID int64
	switchID    int64
	vrfName     string
	// rows 每個 IP 一筆（同 IP 多筆時取 MAC 最小者），依 IP 數值排序，使處理順序固定。
	rows []observation
	// extras 記錄同一 VRF 內同一 IP 出現的其他不同 MAC（arp_sightings 每 VRF 每 IP 只存一筆，
	// 但這些 MAC 仍屬於本輪的 MAC 集合，必須參與衝突判斷）。
	extras map[string][]string
}

// roundPlan 是由 outputs 預先整理出來、與 outputs 順序無關的本輪資料。
type roundPlan struct {
	works []*vrfWork // 依 VRFConfigID 遞增
	// ipMACs 是各 IP 在本輪所有成功 VRF 中看到的不重複 MAC（已排序），首次建立紀錄時以最小者為初始採信 MAC。
	ipMACs map[string][]string
	// winner 是各 IP 擁有最小 MAC 的那筆觀測，供首見事件使用。
	winner map[string]observation
}

// roundState 累積各 VRF 交易「成功提交」後的結果，供第二階段逐 IP 決策使用。
type roundState struct {
	observed map[int64]bool             // 本輪有被看到的 IP 紀錄
	removed  map[int64]bool             // 本輪因超過寬限而被移除觀測的 IP 紀錄
	extras   map[string]map[string]bool // ip -> 同 VRF 內重複出現的其他 MAC
}

// vrfResult 是單一 VRF 交易提交後回報的受影響 IP 紀錄。
type vrfResult struct {
	observed []int64
	removed  []int64
}

// ReconcileObservations 實作 pipeline.ReconcilerProcessor。
func (s *Service) ReconcileObservations(ctx context.Context, outputs []*interfaces.PollOutput, failedVRFConfigIDs []int64) error {
	if s.db == nil {
		return nil
	}
	if s.resolver == nil {
		return errors.New("整併服務缺少網段解析器，無法判定管理範圍")
	}

	now := s.clock.Now()
	graceRounds := 2
	if s.settings != nil {
		graceRounds = s.settings.GetInt(ctx, "offline_grace_rounds", 2)
	}
	if graceRounds < 1 {
		graceRounds = 1
	}

	// 讀取失敗的 VRFConfigID 完全不處理：既不更新觀測，也不累計未見輪數（不造成誤判離線）。
	failedSet := make(map[int64]bool, len(failedVRFConfigIDs))
	for _, id := range failedVRFConfigIDs {
		failedSet[id] = true
	}

	plan := buildPlan(outputs, failedSet)
	if len(plan.works) == 0 {
		return nil
	}

	state := &roundState{
		observed: make(map[int64]bool),
		removed:  make(map[int64]bool),
		extras:   make(map[string]map[string]bool),
	}

	// 階段一：逐 VRF 套用觀測與寬限。單一 VRF 失敗只回滾自己，不影響其他 VRF。
	var errs []error
	for _, w := range plan.works {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		res, err := s.applyVRF(ctx, w, plan, graceRounds, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("VRF 讀取設定 %d 整併失敗: %w", w.vrfConfigID, err))
			continue
		}
		for _, id := range res.observed {
			state.observed[id] = true
		}
		for _, id := range res.removed {
			state.removed[id] = true
		}
		for ip, macs := range w.extras {
			set := state.extras[ip]
			if set == nil {
				set = make(map[string]bool)
				state.extras[ip] = set
			}
			for _, m := range macs {
				set[m] = true
			}
		}
	}

	// 階段二：所有 VRF 與寬限都處理完之後，才逐 IP 決策。
	if err := s.decideIPs(ctx, state, now); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// buildPlan 把 outputs 整理成與順序無關的本輪資料：
// 略過 nil 與讀取失敗的 VRF、同 VRFConfigID 的輸出合併、IPv4 與 MAC 正規化、同 VRF 內同 IP 去重。
func buildPlan(outputs []*interfaces.PollOutput, failed map[int64]bool) *roundPlan {
	type rawWork struct {
		work *vrfWork
		obs  []interfaces.ARPObservation
	}
	merged := make(map[int64]*rawWork)
	var ids []int64
	for _, out := range outputs {
		if out == nil || failed[out.VRFConfigID] {
			continue
		}
		rw, ok := merged[out.VRFConfigID]
		if !ok {
			rw = &rawWork{work: &vrfWork{
				vrfConfigID: out.VRFConfigID,
				switchID:    out.SwitchID,
				vrfName:     out.VRFName,
				extras:      make(map[string][]string),
			}}
			merged[out.VRFConfigID] = rw
			ids = append(ids, out.VRFConfigID)
		}
		rw.obs = append(rw.obs, out.ARPObservations...)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	plan := &roundPlan{
		ipMACs: make(map[string][]string),
		winner: make(map[string]observation),
	}
	macSets := make(map[string]map[string]bool)

	for _, id := range ids {
		rw := merged[id]
		byIP := make(map[string][]observation)
		for _, o := range rw.obs {
			parsed := net.ParseIP(strings.TrimSpace(o.IPAddress)).To4()
			if parsed == nil {
				continue
			}
			mac := strings.ToLower(strings.TrimSpace(o.MACAddress))
			if mac == "" {
				// 沒有 MAC 的項目（例如未完成的 ARP）無法形成觀測，略過。
				continue
			}
			arpType := o.ARPType
			if arpType == "" {
				arpType = "dynamic"
			}
			ip := parsed.String()
			byIP[ip] = append(byIP[ip], observation{
				ip:      ip,
				ipNum:   binary.BigEndian.Uint32(parsed),
				mac:     mac,
				ifIndex: o.IfIndex,
				ifName:  o.IfName,
				arpType: arpType,
			})
		}

		w := rw.work
		for ip, list := range byIP {
			sort.Slice(list, func(i, j int) bool {
				if list[i].mac != list[j].mac {
					return list[i].mac < list[j].mac
				}
				if list[i].ifName != list[j].ifName {
					return list[i].ifName < list[j].ifName
				}
				return list[i].ifIndex < list[j].ifIndex
			})
			best := list[0]
			w.rows = append(w.rows, best)

			set := macSets[ip]
			if set == nil {
				set = make(map[string]bool)
				macSets[ip] = set
			}
			set[best.mac] = true
			if cur, ok := plan.winner[ip]; !ok || best.mac < cur.mac {
				plan.winner[ip] = best
			}
			for _, o := range list[1:] {
				if o.mac == best.mac {
					continue
				}
				set[o.mac] = true
				if !containsString(w.extras[ip], o.mac) {
					w.extras[ip] = append(w.extras[ip], o.mac)
				}
			}
		}
		sort.Slice(w.rows, func(i, j int) bool { return w.rows[i].ipNum < w.rows[j].ipNum })
		plan.works = append(plan.works, w)
	}

	for ip, set := range macSets {
		plan.ipMACs[ip] = sortedKeys(set)
	}
	return plan
}

// target 是 VRF 交易中一筆具備 IP 紀錄資格的觀測，以及它對應的 IP 紀錄。
type target struct {
	obs      observation
	subnetID int64
	id       int64 // IP 紀錄 ID；新位址在插入後才填入
	isNew    bool
}

// applyVRF 在單一交易內處理一個 VRF 的本輪觀測，成功提交後回報受影響的 IP 紀錄。
//
// 交易一開始先以 LockSnapshot 鎖定範圍並讀取當下資格（鎖次序：範圍 → IP 紀錄 ID 遞增），
// 不使用交易外可能過時的快照插入 IP。
func (s *Service) applyVRF(ctx context.Context, w *vrfWork, plan *roundPlan, graceRounds int, now time.Time) (vrfResult, error) {
	var res vrfResult

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("啟動交易失敗: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	snap, err := s.resolver.LockSnapshot(ctx, tx)
	if err != nil {
		return res, err
	}

	insIPStmt, err := tx.PrepareContext(ctx, `INSERT INTO ip_records (ip_address, mac_address, subnet_id, presence, registration, vendor_name, first_seen_at, last_seen_at, created_at, updated_at) VALUES (?, ?, ?, 'online', 'unregistered', ?, ?, ?, ?, ?)`)
	if err != nil {
		return res, fmt.Errorf("準備 IP 紀錄新增語句失敗: %w", err)
	}
	defer insIPStmt.Close()
	insEvtStmt, err := tx.PrepareContext(ctx, `INSERT INTO ip_events (ip_record_id, ip_address, event_type, details, occurred_at) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return res, fmt.Errorf("準備事件新增語句失敗: %w", err)
	}
	defer insEvtStmt.Close()
	upsertSightStmt, err := tx.PrepareContext(ctx, `INSERT INTO arp_sightings (ip_record_id, vrf_config_id, switch_id, vrf_name, mac_address, if_index, if_name, arp_type, last_seen_at, missed_rounds, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?) ON DUPLICATE KEY UPDATE mac_address = VALUES(mac_address), if_index = VALUES(if_index), if_name = VALUES(if_name), arp_type = VALUES(arp_type), last_seen_at = VALUES(last_seen_at), missed_rounds = 0, updated_at = VALUES(updated_at)`)
	if err != nil {
		return res, fmt.Errorf("準備觀測寫入語句失敗: %w", err)
	}
	defer upsertSightStmt.Close()
	undetStmt, err := tx.PrepareContext(ctx, `INSERT INTO undetermined_prefixes (ip_address, mac_address, switch_id, vrf_name, if_name, last_seen_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE mac_address = VALUES(mac_address), last_seen_at = VALUES(last_seen_at), if_name = VALUES(if_name)`)
	if err != nil {
		return res, fmt.Errorf("準備未定前綴寫入語句失敗: %w", err)
	}
	defer undetStmt.Close()

	// 1. 以鎖內快照判定每個位址的歸屬與資格。
	var eligible []target
	for _, row := range w.rows {
		r, err := snap.ResolveIP(row.ip)
		if err != nil {
			return res, err
		}
		if !r.ShouldHaveRecord {
			// 未落在任何已知網段 -> 記入未定前綴清單（閘道、排除範圍、未納管網段內的位址不記）
			if r.SubnetID == 0 {
				if _, err := undetStmt.ExecContext(ctx, row.ip, row.mac, w.switchID, w.vrfName, row.ifName, now, now); err != nil {
					return res, fmt.Errorf("寫入未定前綴 %s 失敗: %w", row.ip, err)
				}
			}
			continue
		}
		eligible = append(eligible, target{obs: row, subnetID: r.SubnetID})
	}

	// 2. 已有納管網段的位址自未定前綴清單移除。
	eligibleIPs := make([]string, len(eligible))
	for i, t := range eligible {
		eligibleIPs[i] = t.obs.ip
	}
	for _, chunk := range chunkStrings(eligibleIPs, sqlChunkSize) {
		if _, err := tx.ExecContext(ctx, `DELETE FROM undetermined_prefixes WHERE ip_address IN (`+placeholders(len(chunk))+`)`, stringArgs(chunk)...); err != nil {
			return res, fmt.Errorf("清除未定前綴失敗: %w", err)
		}
	}

	// 3. 查出既有的 IP 紀錄。既有位址依 ID 遞增處理，新位址排在其後，使加鎖次序固定。
	existing := make(map[string]int64, len(eligible))
	for _, chunk := range chunkStrings(eligibleIPs, sqlChunkSize) {
		rows, err := tx.QueryContext(ctx, `SELECT id, ip_address FROM ip_records WHERE ip_address IN (`+placeholders(len(chunk))+`)`, stringArgs(chunk)...)
		if err != nil {
			return res, fmt.Errorf("查詢既有 IP 紀錄失敗: %w", err)
		}
		for rows.Next() {
			var id int64
			var ip string
			if err := rows.Scan(&id, &ip); err != nil {
				_ = rows.Close()
				return res, fmt.Errorf("讀取既有 IP 紀錄失敗: %w", err)
			}
			existing[ip] = id
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return res, fmt.Errorf("讀取既有 IP 紀錄失敗: %w", err)
		}
		_ = rows.Close()
	}

	var known, fresh []target
	for _, t := range eligible {
		if id, ok := existing[t.obs.ip]; ok {
			t.id = id
			known = append(known, t)
		} else {
			t.isNew = true
			fresh = append(fresh, t)
		}
	}
	sort.SliceStable(known, func(i, j int) bool { return known[i].id < known[j].id })
	ordered := append(known, fresh...)

	// 4. 建立新位址並寫入觀測。
	seen := make(map[int64]bool, len(ordered))
	for _, t := range ordered {
		if t.isNew {
			// 首次出現且有多個 MAC 時，初始採信 MAC 取排序後最小者，不受 outputs 順序影響。
			macs := plan.ipMACs[t.obs.ip]
			adopted := t.obs.mac
			if len(macs) > 0 {
				adopted = macs[0]
			}
			winner, ok := plan.winner[t.obs.ip]
			if !ok {
				winner = t.obs
			}
			insRes, err := insIPStmt.ExecContext(ctx, t.obs.ip, adopted, t.subnetID, s.vendorOf(adopted), now, now, now, now)
			if err != nil {
				return res, fmt.Errorf("建立 IP 紀錄 %s 失敗: %w", t.obs.ip, err)
			}
			t.id, err = insRes.LastInsertId()
			if err != nil {
				return res, fmt.Errorf("取得新 IP 紀錄 ID 失敗: %w", err)
			}
			if _, err := insEvtStmt.ExecContext(ctx, t.id, t.obs.ip, "first_seen", fmt.Sprintf("首次於交換器看到此位址 (MAC: %s, 介面: %s)", adopted, winner.ifName), now); err != nil {
				return res, fmt.Errorf("寫入首見事件 %s 失敗: %w", t.obs.ip, err)
			}
		}
		if _, err := upsertSightStmt.ExecContext(ctx, t.id, w.vrfConfigID, w.switchID, w.vrfName, t.obs.mac, t.obs.ifIndex, t.obs.ifName, t.obs.arpType, now, now, now); err != nil {
			return res, fmt.Errorf("寫入 ARP 觀測 %s 失敗: %w", t.obs.ip, err)
		}
		seen[t.id] = true
		res.observed = append(res.observed, t.id)
	}

	// 5. 本 VRF 原本看得到、本輪未看見的觀測累計未見輪數；達寬限者移除。
	removed, err := s.applyMissed(ctx, tx, w.vrfConfigID, seen, graceRounds, now)
	if err != nil {
		return res, err
	}
	res.removed = removed

	if err := tx.Commit(); err != nil {
		return vrfResult{}, fmt.Errorf("提交交易失敗: %w", err)
	}
	return res, nil
}

// applyMissed 對指定 VRF 中本輪未被看見的既有觀測累計未見輪數，達寬限者移除，回傳被移除觀測所屬的 IP 紀錄 ID。
func (s *Service) applyMissed(ctx context.Context, tx *sql.Tx, vrfConfigID int64, seen map[int64]bool, graceRounds int, now time.Time) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, ip_record_id, missed_rounds FROM arp_sightings WHERE vrf_config_id = ?`, vrfConfigID)
	if err != nil {
		return nil, fmt.Errorf("查詢既有 ARP 觀測失敗: %w", err)
	}
	var bump, drop, dropIPs []int64
	for rows.Next() {
		var sightingID, ipID int64
		var missed int
		if err := rows.Scan(&sightingID, &ipID, &missed); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("讀取既有 ARP 觀測失敗: %w", err)
		}
		if seen[ipID] {
			continue
		}
		if missed+1 >= graceRounds {
			drop = append(drop, sightingID)
			dropIPs = append(dropIPs, ipID)
		} else {
			bump = append(bump, sightingID)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("讀取既有 ARP 觀測失敗: %w", err)
	}
	_ = rows.Close()

	sort.Slice(bump, func(i, j int) bool { return bump[i] < bump[j] })
	sort.Slice(drop, func(i, j int) bool { return drop[i] < drop[j] })

	for _, chunk := range chunkInt64s(bump, sqlChunkSize) {
		args := append([]any{now}, int64Args(chunk)...)
		if _, err := tx.ExecContext(ctx, `UPDATE arp_sightings SET missed_rounds = missed_rounds + 1, updated_at = ? WHERE id IN (`+placeholders(len(chunk))+`)`, args...); err != nil {
			return nil, fmt.Errorf("累計未見輪數失敗: %w", err)
		}
	}
	for _, chunk := range chunkInt64s(drop, sqlChunkSize) {
		if _, err := tx.ExecContext(ctx, `DELETE FROM arp_sightings WHERE id IN (`+placeholders(len(chunk))+`)`, int64Args(chunk)...); err != nil {
			return nil, fmt.Errorf("移除逾時 ARP 觀測失敗: %w", err)
		}
	}
	return dropIPs, nil
}

// ipRecordState 是第二階段在鎖內讀到的 IP 紀錄現況。
type ipRecordState struct {
	id       int64
	ip       string
	presence string
	mac      string
	owned    bool // 以 owner_user_id 判斷，不看 registration 字串
}

// activeConflict 是某 IP 目前未解消（open 或 in_progress）的衝突。
type activeConflict struct {
	id     int64
	status string
}

// decideIPs 對本輪受影響的 IP 紀錄逐筆決策。依 ID 遞增分批，每批一筆交易。
func (s *Service) decideIPs(ctx context.Context, st *roundState, now time.Time) error {
	idSet := make(map[int64]bool, len(st.observed)+len(st.removed))
	for id := range st.observed {
		idSet[id] = true
	}
	for id := range st.removed {
		idSet[id] = true
	}
	if len(idSet) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	var errs []error
	for _, chunk := range chunkInt64s(ids, sqlChunkSize) {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if err := s.decideChunk(ctx, chunk, st, now); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// decideChunk 在單一交易內對一批 IP 紀錄做第二階段決策。
//
// 規則（開發規格 3.4、3.6）：令 S 為該 IP 目前所有觀測（含寬限內者與本輪同 VRF 重複項）的不重複 MAC 集合。
//   - |S| >= 2：開立或更新衝突，採信 MAC 不變。
//   - |S| == 1 且等於採信 MAC：open 衝突自動已解消（auto_vanished），in_progress 衝突解消為 keep_adopted。
//   - |S| == 1 且不等於採信 MAC：有擁有者 -> 開立或更新衝突，登記與採信 MAC 不變；
//     無擁有者 -> 更新採信 MAC 並記 mac_changed（若有 in_progress 衝突則維持管理者「維持原採信」的裁決，不更動）。
//   - |S| == 0：open 衝突保持 open；in_progress 衝突解消為 keep_adopted。
func (s *Service) decideChunk(ctx context.Context, ids []int64, st *roundState, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("啟動逐 IP 決策交易失敗: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	idArgs := int64Args(ids)
	in := placeholders(len(ids))

	// 依 ID 遞增鎖定 IP 紀錄；範圍變更已在階段一鎖內處理，已被刪除的紀錄不會出現在這裡。
	rows, err := tx.QueryContext(ctx, `SELECT id, ip_address, presence, mac_address, (owner_user_id IS NOT NULL) FROM ip_records WHERE id IN (`+in+`) ORDER BY id FOR UPDATE`, idArgs...)
	if err != nil {
		return fmt.Errorf("鎖定 IP 紀錄失敗: %w", err)
	}
	var records []ipRecordState
	for rows.Next() {
		var r ipRecordState
		if err := rows.Scan(&r.id, &r.ip, &r.presence, &r.mac, &r.owned); err != nil {
			_ = rows.Close()
			return fmt.Errorf("讀取 IP 紀錄失敗: %w", err)
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("讀取 IP 紀錄失敗: %w", err)
	}
	_ = rows.Close()
	if len(records) == 0 {
		return nil
	}

	liveIDs := make([]int64, len(records))
	for i, r := range records {
		liveIDs[i] = r.id
	}
	liveIn := placeholders(len(liveIDs))
	liveArgs := int64Args(liveIDs)

	// 讀取每個 IP 目前剩餘的觀測 MAC。
	sightMACs := make(map[int64]map[string]bool, len(records))
	sRows, err := tx.QueryContext(ctx, `SELECT ip_record_id, mac_address FROM arp_sightings WHERE ip_record_id IN (`+liveIn+`)`, liveArgs...)
	if err != nil {
		return fmt.Errorf("讀取 ARP 觀測失敗: %w", err)
	}
	for sRows.Next() {
		var ipID int64
		var mac string
		if err := sRows.Scan(&ipID, &mac); err != nil {
			_ = sRows.Close()
			return fmt.Errorf("讀取 ARP 觀測失敗: %w", err)
		}
		set := sightMACs[ipID]
		if set == nil {
			set = make(map[string]bool)
			sightMACs[ipID] = set
		}
		set[strings.ToLower(mac)] = true
	}
	if err := sRows.Err(); err != nil {
		_ = sRows.Close()
		return fmt.Errorf("讀取 ARP 觀測失敗: %w", err)
	}
	_ = sRows.Close()

	// 讀取（並鎖定）未解消的衝突。同一 IP 同時只會有一筆 open／in_progress。
	conflicts := make(map[int64]activeConflict)
	cRows, err := tx.QueryContext(ctx, `SELECT id, ip_record_id, status FROM ip_conflicts WHERE ip_record_id IN (`+liveIn+`) AND status IN ('open', 'in_progress') ORDER BY id FOR UPDATE`, liveArgs...)
	if err != nil {
		return fmt.Errorf("讀取衝突失敗: %w", err)
	}
	for cRows.Next() {
		var c activeConflict
		var ipID int64
		if err := cRows.Scan(&c.id, &ipID, &c.status); err != nil {
			_ = cRows.Close()
			return fmt.Errorf("讀取衝突失敗: %w", err)
		}
		conflicts[ipID] = c
	}
	if err := cRows.Err(); err != nil {
		_ = cRows.Close()
		return fmt.Errorf("讀取衝突失敗: %w", err)
	}
	_ = cRows.Close()

	insEvtStmt, err := tx.PrepareContext(ctx, `INSERT INTO ip_events (ip_record_id, ip_address, event_type, details, occurred_at) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("準備事件新增語句失敗: %w", err)
	}
	defer insEvtStmt.Close()
	setOfflineStmt, err := tx.PrepareContext(ctx, `UPDATE ip_records SET presence = 'offline', updated_at = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("準備離線更新語句失敗: %w", err)
	}
	defer setOfflineStmt.Close()
	adoptStmt, err := tx.PrepareContext(ctx, `UPDATE ip_records SET mac_address = ?, vendor_name = ?, updated_at = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("準備採信 MAC 更新語句失敗: %w", err)
	}
	defer adoptStmt.Close()
	upsertConflictStmt, err := tx.PrepareContext(ctx, `INSERT INTO ip_conflicts (ip_record_id, ip_address, adopted_mac, conflict_macs, status, first_detected_at, last_detected_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'open', ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE adopted_mac = VALUES(adopted_mac), conflict_macs = VALUES(conflict_macs), last_detected_at = VALUES(last_detected_at), updated_at = VALUES(updated_at)`)
	if err != nil {
		return fmt.Errorf("準備衝突寫入語句失敗: %w", err)
	}
	defer upsertConflictStmt.Close()
	resolveStmt, err := tx.PrepareContext(ctx, `UPDATE ip_conflicts SET status = 'resolved', resolution = ?, resolved_by = 'system', resolved_at = ?, updated_at = ? WHERE id = ? AND status = ?`)
	if err != nil {
		return fmt.Errorf("準備衝突解消語句失敗: %w", err)
	}
	defer resolveStmt.Close()

	// 本輪有被看到的 IP：一次更新為在線並刷新最後看到時間。
	var observedIDs []int64
	for _, r := range records {
		if st.observed[r.id] {
			observedIDs = append(observedIDs, r.id)
		}
	}
	for _, chunk := range chunkInt64s(observedIDs, sqlChunkSize) {
		args := append([]any{now, now}, int64Args(chunk)...)
		if _, err := tx.ExecContext(ctx, `UPDATE ip_records SET presence = 'online', last_seen_at = ?, updated_at = ? WHERE id IN (`+placeholders(len(chunk))+`)`, args...); err != nil {
			return fmt.Errorf("更新在線狀態失敗: %w", err)
		}
	}

	for _, r := range records {
		observed := st.observed[r.id]

		// MAC 集合 S：剩餘觀測加上本輪同 VRF 內重複出現的其他 MAC。
		macSet := make(map[string]bool, len(sightMACs[r.id])+2)
		for m := range sightMACs[r.id] {
			macSet[m] = true
		}
		if observed {
			for m := range st.extras[r.ip] {
				macSet[m] = true
			}
		}
		macs := sortedKeys(macSet)

		// 在線／離線轉換。離線不等於刪除，也不動登記與擁有者。
		if observed {
			if r.presence == "offline" {
				if _, err := insEvtStmt.ExecContext(ctx, r.id, r.ip, "online", "位址重新上線", now); err != nil {
					return fmt.Errorf("寫入上線事件 %s 失敗: %w", r.ip, err)
				}
			}
		} else if len(sightMACs[r.id]) == 0 && r.presence == "online" {
			if _, err := setOfflineStmt.ExecContext(ctx, now, r.id); err != nil {
				return fmt.Errorf("更新離線狀態 %s 失敗: %w", r.ip, err)
			}
			if _, err := insEvtStmt.ExecContext(ctx, r.id, r.ip, "offline", "所有來源均已超時未見，狀態轉為離線", now); err != nil {
				return fmt.Errorf("寫入離線事件 %s 失敗: %w", r.ip, err)
			}
		}

		// 採信 MAC 為空（理論上不應發生）時，以集合中排序最小者補上，不記變更事件。
		adopted := r.mac
		if adopted == "" && len(macs) > 0 {
			adopted = macs[0]
			if _, err := adoptStmt.ExecContext(ctx, adopted, s.vendorOf(adopted), now, r.id); err != nil {
				return fmt.Errorf("補上採信 MAC %s 失敗: %w", r.ip, err)
			}
		}

		act, hasAct := conflicts[r.id]

		resolve := func(resolution string) error {
			if _, err := resolveStmt.ExecContext(ctx, resolution, now, now, act.id, act.status); err != nil {
				return fmt.Errorf("解消衝突 %s 失敗: %w", r.ip, err)
			}
			return nil
		}
		upsertConflict := func() error {
			macJSON, err := json.Marshal(unionSorted(macs, adopted))
			if err != nil {
				return fmt.Errorf("序列化衝突 MAC %s 失敗: %w", r.ip, err)
			}
			if _, err := upsertConflictStmt.ExecContext(ctx, r.id, r.ip, adopted, string(macJSON), now, now, now, now); err != nil {
				return fmt.Errorf("寫入衝突 %s 失敗: %w", r.ip, err)
			}
			return nil
		}

		switch {
		case len(macs) >= 2:
			// 多個 MAC 並存：開衝突，採信 MAC 不變，與 outputs 順序無關。
			if err := upsertConflict(); err != nil {
				return err
			}

		case len(macs) == 1 && macs[0] == adopted:
			// 集合恰好只剩採信 MAC：另一個 MAC 已消失，衝突自動解消。
			if hasAct {
				resolution := "auto_vanished"
				if act.status == "in_progress" {
					resolution = "keep_adopted"
				}
				if err := resolve(resolution); err != nil {
					return err
				}
			}

		case len(macs) == 1:
			m := macs[0]
			switch {
			case r.owned:
				// 有擁有者的 IP 換了 MAC：開衝突，登記與採信 MAC 不變。
				if err := upsertConflict(); err != nil {
					return err
				}
			case hasAct && act.status == "in_progress":
				// 管理者已裁決維持原採信 MAC，不自動覆寫；衝突維持進行中並刷新內容。
				if err := upsertConflict(); err != nil {
					return err
				}
			default:
				// 未登記的 IP 換 MAC：直接更新採信 MAC 並記歷程，不開衝突。
				if _, err := adoptStmt.ExecContext(ctx, m, s.vendorOf(m), now, r.id); err != nil {
					return fmt.Errorf("更新採信 MAC %s 失敗: %w", r.ip, err)
				}
				if _, err := insEvtStmt.ExecContext(ctx, r.id, r.ip, "mac_changed", fmt.Sprintf("未登記位址 MAC 由 %s 變更為 %s", adopted, m), now); err != nil {
					return fmt.Errorf("寫入 MAC 變更事件 %s 失敗: %w", r.ip, err)
				}
				if hasAct {
					if err := resolve("auto_vanished"); err != nil {
						return err
					}
				}
			}

		default:
			// 集合為空（完全沒有觀測）：open 衝突保持 open，等待回到採信 MAC；
			// 已裁決維持原採信的 in_progress 衝突，在其他 MAC 都消失後解消。
			if hasAct && act.status == "in_progress" {
				if err := resolve("keep_adopted"); err != nil {
					return err
				}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交逐 IP 決策交易失敗: %w", err)
	}
	return nil
}

// vendorOf 以 OUI 對照表查詢廠商名稱，查不到或未設定時回傳「未知」。
func (s *Service) vendorOf(mac string) string {
	if s.ouiLookup == nil {
		return "未知"
	}
	return s.ouiLookup.Lookup(mac)
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// unionSorted 回傳 macs 與 extra 的聯集（不重複、已排序、略過空字串）。
func unionSorted(macs []string, extra string) []string {
	set := make(map[string]bool, len(macs)+1)
	for _, m := range macs {
		set[m] = true
	}
	if extra != "" {
		set[extra] = true
	}
	return sortedKeys(set)
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func stringArgs(list []string) []any {
	args := make([]any, len(list))
	for i, v := range list {
		args[i] = v
	}
	return args
}

func int64Args(list []int64) []any {
	args := make([]any, len(list))
	for i, v := range list {
		args[i] = v
	}
	return args
}

func chunkStrings(list []string, size int) [][]string {
	var out [][]string
	for len(list) > 0 {
		n := size
		if n > len(list) {
			n = len(list)
		}
		out = append(out, list[:n])
		list = list[n:]
	}
	return out
}

func chunkInt64s(list []int64, size int) [][]int64 {
	var out [][]int64
	for len(list) > 0 {
		n := size
		if n > len(list) {
			n = len(list)
		}
		out = append(out, list[:n])
		list = list[n:]
	}
	return out
}
