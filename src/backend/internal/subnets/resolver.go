package subnets

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

// IPResolutionResult 封裝單一 IPv4 經過網段、排除範圍與閘道判定後的歸屬結果。
type IPResolutionResult struct {
	SubnetID         int64
	SubnetCIDR       string
	SubnetState      string // pending, managed, unmanaged
	IsGateway        bool
	IsExcluded       bool
	ExcludeReason    string
	ShouldHaveRecord bool
}

type subnetEntry struct {
	id      int64
	cidr    string
	state   string
	ipNet   *net.IPNet
	maskLen int
}

type exclusionEntry struct {
	startUint uint32
	endUint   uint32
	reason    string
}

// Snapshot 封裝網段、閘道與排除範圍的記憶體快照，支援高效批次最長前綴比對（LPM）。
type Snapshot struct {
	gateways   map[string]int64
	subnets    []subnetEntry
	exclusions map[int64][]exclusionEntry
}

// ResolveIP 在記憶體快照中執行單一 IPv4 歸屬與資格判定，零 SQL 往返。
func (s *Snapshot) ResolveIP(ipStr string) (IPResolutionResult, error) {
	parsedIP := net.ParseIP(ipStr).To4()
	if parsedIP == nil {
		return IPResolutionResult{}, fmt.Errorf("無效的 IPv4 位址: %s", ipStr)
	}
	ipUint := binary.BigEndian.Uint32(parsedIP)

	if gwSubID, ok := s.gateways[ipStr]; ok {
		return IPResolutionResult{
			SubnetID:         gwSubID,
			IsGateway:        true,
			ShouldHaveRecord: false,
		}, nil
	}

	var matched *subnetEntry
	maxMask := -1
	for i := range s.subnets {
		sub := &s.subnets[i]
		if sub.ipNet.Contains(parsedIP) && sub.maskLen > maxMask {
			maxMask = sub.maskLen
			matched = sub
		}
	}

	if matched == nil {
		return IPResolutionResult{ShouldHaveRecord: false}, nil
	}

	for _, ex := range s.exclusions[matched.id] {
		if ipUint >= ex.startUint && ipUint <= ex.endUint {
			return IPResolutionResult{
				SubnetID:         matched.id,
				SubnetCIDR:       matched.cidr,
				SubnetState:      matched.state,
				IsExcluded:       true,
				ExcludeReason:    ex.reason,
				ShouldHaveRecord: false,
			}, nil
		}
	}

	return IPResolutionResult{
		SubnetID:         matched.id,
		SubnetCIDR:       matched.cidr,
		SubnetState:      matched.state,
		ShouldHaveRecord: matched.state == "managed",
	}, nil
}

// Resolver 負責查詢 IPv4 在系統中的網段歸屬與管理範圍資格。
type Resolver struct {
	db *sql.DB
}

func NewResolver(db *sql.DB) *Resolver {
	return &Resolver{db: db}
}

// scopeQuerier 抽象出「可執行查詢」的物件，讓同一份載入邏輯可在 *sql.DB（一般讀取）
// 或 *sql.Tx（鎖內讀取）上重複使用。
type scopeQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// scopeRow 是 subnets 表中與範圍判定有關的欄位。
type scopeRow struct {
	id    int64
	cidr  string
	state string
}

// 鎖內讀取使用的尾綴：
//   - 網段列以 FOR UPDATE（排他）鎖定，並依 id 排序，使所有取鎖者以相同次序加鎖；
//   - 閘道與排除範圍以 LOCK IN SHARE MODE 讀取「最新已提交」資料，
//     避免交易先前已建立的 REPEATABLE READ 一致性讀取視圖讓快照過時。
const (
	lockScopeRowsSQL    = `SELECT id, cidr, state FROM subnets ORDER BY id FOR UPDATE`
	plainScopeRowsSQL   = `SELECT id, cidr, state FROM subnets ORDER BY id`
	gatewaysSQL         = `SELECT ip_address, subnet_id FROM subnet_gateways`
	exclusionsSQL       = `SELECT subnet_id, start_ip, end_ip, reason FROM exclusion_ranges ORDER BY id`
	shareModeLockSuffix = ` LOCK IN SHARE MODE`
)

// queryScopeRows 讀取網段列；所有 Scan 與迭代錯誤都會回傳，rows 一律關閉。
func queryScopeRows(ctx context.Context, q scopeQuerier, query string) ([]scopeRow, error) {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []scopeRow
	for rows.Next() {
		var row scopeRow
		if err := rows.Scan(&row.id, &row.cidr, &row.state); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// lockScopeRows 在交易內以 id 遞增次序對「所有」網段列加排他鎖並回傳其內容。
//
// 這是範圍鎖的唯一入口：Resolver.LockSnapshot 與所有範圍變更（CIDR、狀態、排除範圍）
// 都先走這裡，因此彼此以相同的次序取鎖，不會互相死結。全表掃描加鎖亦會鎖住最後一列之後的間隙
// （含空表時的 supremum），所以同時保護「重疊的新 CIDR」與「尚無任何網段」時的併發新增。
func lockScopeRows(ctx context.Context, tx *sql.Tx) ([]scopeRow, error) {
	return queryScopeRows(ctx, tx, lockScopeRowsSQL)
}

// loadSnapshot 以給定的網段列載入閘道與排除範圍並組成快照。
// 任何查詢、Scan、迭代錯誤，以及資料庫中無法解析的 CIDR／IP 都視為錯誤（fail-closed）：
// 無法確認的範圍資料不可被當成「沒有這筆網段」而靜默略過。
func loadSnapshot(ctx context.Context, q scopeQuerier, subnetRows []scopeRow, shareLock bool) (*Snapshot, error) {
	snap := &Snapshot{
		gateways:   make(map[string]int64),
		exclusions: make(map[int64][]exclusionEntry),
	}

	for _, row := range subnetRows {
		_, ipNet, err := net.ParseCIDR(row.cidr)
		if err != nil || ipNet.IP.To4() == nil {
			return nil, fmt.Errorf("網段 %d 的 CIDR %q 無法解析", row.id, row.cidr)
		}
		ones, _ := ipNet.Mask.Size()
		snap.subnets = append(snap.subnets, subnetEntry{
			id: row.id, cidr: row.cidr, state: row.state, ipNet: ipNet, maskLen: ones,
		})
	}

	suffix := ""
	if shareLock {
		suffix = shareModeLockSuffix
	}

	if err := loadGateways(ctx, q, gatewaysSQL+suffix, snap); err != nil {
		return nil, fmt.Errorf("載入閘道位址失敗: %w", err)
	}
	if err := loadExclusions(ctx, q, exclusionsSQL+suffix, snap); err != nil {
		return nil, fmt.Errorf("載入排除範圍失敗: %w", err)
	}
	return snap, nil
}

func loadGateways(ctx context.Context, q scopeQuerier, query string, snap *Snapshot) error {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ip string
		var subID int64
		if err := rows.Scan(&ip, &subID); err != nil {
			return err
		}
		snap.gateways[ip] = subID
	}
	return rows.Err()
}

func loadExclusions(ctx context.Context, q scopeQuerier, query string, snap *Snapshot) error {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var subID int64
		var sStr, eStr, reason string
		if err := rows.Scan(&subID, &sStr, &eStr, &reason); err != nil {
			return err
		}
		sIP := net.ParseIP(sStr).To4()
		eIP := net.ParseIP(eStr).To4()
		if sIP == nil || eIP == nil {
			return fmt.Errorf("網段 %d 的排除範圍 %q-%q 不是有效的 IPv4", subID, sStr, eStr)
		}
		snap.exclusions[subID] = append(snap.exclusions[subID], exclusionEntry{
			startUint: binary.BigEndian.Uint32(sIP),
			endUint:   binary.BigEndian.Uint32(eIP),
			reason:    reason,
		})
	}
	return rows.Err()
}

// BuildSnapshot 從資料庫一次性載入網段、閘道與排除範圍至記憶體（不加鎖，供唯讀判定使用）。
// 需要「判定後寫入」的流程（例如對帳）請改用 LockSnapshot，否則快照可能在寫入前就已過時。
func (r *Resolver) BuildSnapshot(ctx context.Context) (*Snapshot, error) {
	if r.db == nil {
		return nil, errors.New("資料庫未連線")
	}
	subnetRows, err := queryScopeRows(ctx, r.db, plainScopeRowsSQL)
	if err != nil {
		return nil, fmt.Errorf("載入網段失敗: %w", err)
	}
	return loadSnapshot(ctx, r.db, subnetRows, false)
}

// LockScope 只取得與 LockSnapshot 相同的範圍鎖（subnets 全部列依 id FOR UPDATE），不載入快照。
// 給「只需遵守 scope → IP 紀錄加鎖次序、本身不依賴範圍判定」的交易使用（例如清除排程）。
func (r *Resolver) LockScope(ctx context.Context, tx *sql.Tx) error {
	if tx == nil {
		return errors.New("LockScope 需要有效的交易")
	}
	if _, err := lockScopeRows(ctx, tx); err != nil {
		return fmt.Errorf("鎖定網段範圍失敗: %w", err)
	}
	return nil
}

// LockSnapshot 在呼叫端的交易內鎖定所有範圍列，並以交易目前看到的最新資料建立快照。
//
// 鎖次序（與所有範圍變更一致，不可顛倒）：
//  1. subnets 全部列，依 id 遞增 FOR UPDATE；
//  2. subnet_gateways、exclusion_ranges 以 LOCK IN SHARE MODE 讀取最新已提交內容；
//  3. 之後才寫入 ip_records 等業務表。
//
// 回傳的快照在交易結束（Commit／Rollback）前不會被 CIDR、狀態、排除範圍的變更改動，
// 因此「依快照判定、再寫入 IP 紀錄」不會重建剛被範圍變更刪除的紀錄。
// 呼叫端應在該筆 VRF 交易的一開始呼叫、且不要在持鎖期間做 SNMP 等外部 I/O；
// 鎖在交易結束時釋放，本函式不使用全域 mutex。
// 若 tx 為 nil，或任何查詢、Scan、迭代出錯，都回傳錯誤（交易應回滾）。
func (r *Resolver) LockSnapshot(ctx context.Context, tx *sql.Tx) (*Snapshot, error) {
	if tx == nil {
		return nil, errors.New("LockSnapshot 需要有效的交易")
	}
	subnetRows, err := lockScopeRows(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("鎖定網段範圍失敗: %w", err)
	}
	return loadSnapshot(ctx, tx, subnetRows, true)
}

// Resolve 解析指定 IPv4 位址之歸屬。
func (r *Resolver) Resolve(ctx context.Context, ipStr string) (IPResolutionResult, error) {
	snap, err := r.BuildSnapshot(ctx)
	if err != nil {
		return IPResolutionResult{}, err
	}
	return snap.ResolveIP(ipStr)
}
