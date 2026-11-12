package reconciliation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/settings"
	"github.com/jeffhou369/wurzburg/backend/internal/subnets"
)

// Service 實作將一輪 ARP 觀測整併進 IP 紀錄、多來源在線／離線判定與 MAC 衝突偵測（Ticket 13 & 25）。
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

// ReconcileObservations 實作 pipeline.ReconcilerProcessor。
func (s *Service) ReconcileObservations(ctx context.Context, outputs []*interfaces.PollOutput, failedVRFConfigIDs []int64) error {
	if s.db == nil {
		return nil
	}

	now := s.clock.Now()
	graceRounds := 2
	if s.settings != nil {
		graceRounds = s.settings.GetInt(ctx, "offline_grace_rounds", 2)
	}
	if graceRounds < 1 {
		graceRounds = 1
	}

	// 排除讀取失敗的 VRFConfigID（讀取失敗的輪次絕不增加未見輪數，不造成誤判離線）
	failedSet := make(map[int64]bool)
	for _, id := range failedVRFConfigIDs {
		failedSet[id] = true
	}

	for _, out := range outputs {
		if out == nil || failedSet[out.VRFConfigID] {
			continue
		}

		seenKeysInThisOutput := make(map[string]bool)

		for _, obs := range out.ARPObservations {
			cleanIP := strings.TrimSpace(obs.IPAddress)
			if net.ParseIP(cleanIP).To4() == nil {
				continue
			}
			mac := strings.ToLower(strings.TrimSpace(obs.MACAddress))

			// 1. 查詢該位址在管理範圍中的歸屬
			res, err := s.resolver.Resolve(ctx, cleanIP)
			if err != nil {
				continue
			}

			if !res.ShouldHaveRecord {
				// 未落在任何已知網段 -> 記入未定前綴清單
				if res.SubnetID == 0 {
					_, _ = s.db.ExecContext(
						ctx,
						`INSERT INTO undetermined_prefixes (ip_address, mac_address, switch_id, vrf_name, if_name, last_seen_at, created_at)
						 VALUES (?, ?, ?, ?, ?, ?, ?)
						 ON DUPLICATE KEY UPDATE mac_address = VALUES(mac_address), last_seen_at = VALUES(last_seen_at), if_name = VALUES(if_name)`,
						cleanIP, mac, out.SwitchID, out.VRFName, obs.IfName, now, now,
					)
				}
				continue
			}

			// 若原本在未定前綴清單，現在已有納管網段 -> 自未定前綴清單移除
			_, _ = s.db.ExecContext(ctx, `DELETE FROM undetermined_prefixes WHERE ip_address = ?`, cleanIP)

			// 2. 查詢或建立 IP 紀錄
			var ipRecordID int64
			var currentPresence, currentReg, adoptedMAC string
			err = s.db.QueryRowContext(
				ctx,
				`SELECT id, presence, registration, mac_address FROM ip_records WHERE ip_address = ?`,
				cleanIP,
			).Scan(&ipRecordID, &currentPresence, &currentReg, &adoptedMAC)

			if errors.Is(err, sql.ErrNoRows) {
				vendor := "未知"
				if s.ouiLookup != nil {
					vendor = s.ouiLookup.Lookup(mac)
				}
				insRes, insErr := s.db.ExecContext(
					ctx,
					`INSERT INTO ip_records (ip_address, mac_address, subnet_id, presence, registration, vendor_name, first_seen_at, last_seen_at, created_at, updated_at)
					 VALUES (?, ?, ?, 'online', 'unregistered', ?, ?, ?, ?, ?)`,
					cleanIP, mac, res.SubnetID, vendor, now, now, now, now,
				)
				if insErr != nil {
					continue
				}
				ipRecordID, _ = insRes.LastInsertId()
				adoptedMAC = mac
				currentPresence = "online"
				currentReg = "unregistered"

				_, _ = s.db.ExecContext(
					ctx,
					`INSERT INTO ip_events (ip_record_id, ip_address, event_type, details, occurred_at) VALUES (?, ?, 'first_seen', ?, ?)`,
					ipRecordID, cleanIP, fmt.Sprintf("首次於交換器看到此位址 (MAC: %s, 介面: %s)", mac, obs.IfName), now,
				)
			} else if err == nil {
				// 既有紀錄若原為離線，立即恢復在線
				if currentPresence == "offline" {
					_, _ = s.db.ExecContext(ctx, `UPDATE ip_records SET presence = 'online', last_seen_at = ?, updated_at = ? WHERE id = ?`, now, now, ipRecordID)
					_, _ = s.db.ExecContext(
						ctx,
						`INSERT INTO ip_events (ip_record_id, ip_address, event_type, details, occurred_at) VALUES (?, ?, 'online', '位址重新上線', ?)`,
						ipRecordID, cleanIP, now,
					)
				} else {
					_, _ = s.db.ExecContext(ctx, `UPDATE ip_records SET last_seen_at = ?, updated_at = ? WHERE id = ?`, now, now, ipRecordID)
				}

				// MAC 變更檢驗（衝突 vs 直接更新）
				if adoptedMAC != "" && mac != adoptedMAC {
					if currentReg == "unregistered" {
						// 未登記位址換了 MAC -> 直接更新採信 MAC 並記歷程，不開衝突 (Ticket 13 規定)
						vendor := "未知"
						if s.ouiLookup != nil {
							vendor = s.ouiLookup.Lookup(mac)
						}
						_, _ = s.db.ExecContext(ctx, `UPDATE ip_records SET mac_address = ?, vendor_name = ?, updated_at = ? WHERE id = ?`, mac, vendor, now, ipRecordID)
						_, _ = s.db.ExecContext(
							ctx,
							`INSERT INTO ip_events (ip_record_id, ip_address, event_type, details, occurred_at) VALUES (?, ?, 'mac_changed', ?, ?)`,
							ipRecordID, cleanIP, fmt.Sprintf("未登記位址 MAC 由 %s 變更為 %s", adoptedMAC, mac), now,
						)
						adoptedMAC = mac
					} else {
						// 已登記或已到期位址換了 MAC -> 開啟或更新衝突 (Ticket 25)
						s.recordConflict(ctx, ipRecordID, cleanIP, adoptedMAC, []string{adoptedMAC, mac}, now)
					}
				}
			}

			// 3. 儲存目前的 ARP 觀測
			arpType := obs.ARPType
			if arpType == "" {
				arpType = "dynamic"
			}
			_, _ = s.db.ExecContext(
				ctx,
				`INSERT INTO arp_sightings (ip_record_id, vrf_config_id, switch_id, vrf_name, mac_address, if_index, if_name, arp_type, last_seen_at, missed_rounds, created_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
				 ON DUPLICATE KEY UPDATE mac_address = VALUES(mac_address), if_index = VALUES(if_index), if_name = VALUES(if_name), arp_type = VALUES(arp_type), last_seen_at = VALUES(last_seen_at), missed_rounds = 0, updated_at = VALUES(updated_at)`,
				ipRecordID, out.VRFConfigID, out.SwitchID, out.VRFName, mac, obs.IfIndex, obs.IfName, arpType, now, now, now,
			)

			seenKeysInThisOutput[fmt.Sprintf("%d:%d", ipRecordID, out.VRFConfigID)] = true
		}

		// 4. 針對本輸出中「原本看得到、但本輪未看見」的觀測累計未見輪數
		s.handleMissedSightings(ctx, out.VRFConfigID, seenKeysInThisOutput, graceRounds, now)
	}

	// 5. 巡檢多 MAC 並存衝突與自動已解消判定
	s.checkActiveConflicts(ctx, now, graceRounds)
	return nil
}

func (s *Service) handleMissedSightings(ctx context.Context, vrfConfigID int64, seen map[string]bool, graceRounds int, now time.Time) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, ip_record_id, missed_rounds FROM arp_sightings WHERE vrf_config_id = ?`, vrfConfigID)
	if err != nil {
		return
	}
	defer rows.Close()

	type missedInfo struct {
		sightingID int64
		ipRecordID int64
		rounds     int
	}
	var missedList []missedInfo

	for rows.Next() {
		var sID, ipID int64
		var mRounds int
		if rows.Scan(&sID, &ipID, &mRounds) == nil {
			key := fmt.Sprintf("%d:%d", ipID, vrfConfigID)
			if !seen[key] {
				missedList = append(missedList, missedInfo{sightingID: sID, ipRecordID: ipID, rounds: mRounds + 1})
			}
		}
	}
	_ = rows.Close()

	for _, m := range missedList {
		if m.rounds >= graceRounds {
			// 超過寬限輪數 -> 移除該筆觀測
			_, _ = s.db.ExecContext(ctx, `DELETE FROM arp_sightings WHERE id = ?`, m.sightingID)

			// 檢查該 IP 紀錄是否還有其他來源的觀測
			var remaining int
			_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM arp_sightings WHERE ip_record_id = ?`, m.ipRecordID).Scan(&remaining)
			if remaining == 0 {
				var cleanIP string
				_ = s.db.QueryRowContext(ctx, `SELECT ip_address FROM ip_records WHERE id = ?`, m.ipRecordID).Scan(&cleanIP)

				_, _ = s.db.ExecContext(ctx, `UPDATE ip_records SET presence = 'offline', updated_at = ? WHERE id = ?`, now, m.ipRecordID)
				_, _ = s.db.ExecContext(
					ctx,
					`INSERT INTO ip_events (ip_record_id, ip_address, event_type, details, occurred_at) VALUES (?, ?, 'offline', '所有來源均已超時未見，狀態轉為離線', ?)`,
					m.ipRecordID, cleanIP, now,
				)
			}
		} else {
			_, _ = s.db.ExecContext(ctx, `UPDATE arp_sightings SET missed_rounds = ?, updated_at = ? WHERE id = ?`, m.rounds, now, m.sightingID)
		}
	}
}

func (s *Service) recordConflict(ctx context.Context, ipRecordID int64, ip, adoptedMAC string, macs []string, now time.Time) {
	macJSON, _ := json.Marshal(macs)
	_, _ = s.db.ExecContext(
		ctx,
		`INSERT INTO ip_conflicts (ip_record_id, ip_address, adopted_mac, conflict_macs, status, first_detected_at, last_detected_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'open', ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE conflict_macs = VALUES(conflict_macs), last_detected_at = VALUES(last_detected_at), updated_at = VALUES(updated_at)`,
		ipRecordID, ip, adoptedMAC, string(macJSON), now, now, now, now,
	)
}

func (s *Service) checkActiveConflicts(ctx context.Context, now time.Time, _ int) {
	// 巡檢同 IP 存在 2 個以上不同 MAC 之在線觀測（並存衝突）
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT s.ip_record_id, r.ip_address, r.mac_address, GROUP_CONCAT(DISTINCT s.mac_address) AS macs, COUNT(DISTINCT s.mac_address) AS cnt
		 FROM arp_sightings s
		 JOIN ip_records r ON s.ip_record_id = r.id
		 GROUP BY s.ip_record_id, r.ip_address, r.mac_address
		 HAVING cnt > 1`,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var ipID int64
			var ip, adopted, macListStr string
			var count int
			if rows.Scan(&ipID, &ip, &adopted, &macListStr, &count) == nil {
				macs := strings.Split(macListStr, ",")
				s.recordConflict(ctx, ipID, ip, adopted, macs, now)
			}
		}
	}

	// 檢查已開立衝突中，不一致是否已自行消失（集合恢復成只有採信 MAC 或只有單一 MAC）
	confRows, err := s.db.QueryContext(ctx, `SELECT id, ip_record_id, adopted_mac, status FROM ip_conflicts WHERE status IN ('open', 'in_progress')`)
	if err == nil {
		defer confRows.Close()
		type confCheck struct {
			confID  int64
			ipID    int64
			adopted string
			status  string
		}
		var checkList []confCheck
		for confRows.Next() {
			var c confCheck
			if confRows.Scan(&c.confID, &c.ipID, &c.adopted, &c.status) == nil {
				checkList = append(checkList, c)
			}
		}
		_ = confRows.Close()

		for _, c := range checkList {
			sRows, err := s.db.QueryContext(ctx, `SELECT DISTINCT mac_address FROM arp_sightings WHERE ip_record_id = ?`, c.ipID)
			if err != nil {
				continue
			}
			var liveMACs []string
			for sRows.Next() {
				var m string
				if sRows.Scan(&m) == nil {
					liveMACs = append(liveMACs, m)
				}
			}
			_ = sRows.Close()

			if len(liveMACs) <= 1 {
				// 若目前只剩 1 個或 0 個 MAC
				if len(liveMACs) == 0 || (len(liveMACs) == 1 && liveMACs[0] == c.adopted) {
					// 另一個 MAC 消失，衝突已自動解消
					resolution := "auto_vanished"
					if c.status == "in_progress" {
						resolution = "keep_adopted"
					}
					_, _ = s.db.ExecContext(
						ctx,
						`UPDATE ip_conflicts SET status = 'resolved', resolution = ?, resolved_by = 'system', resolved_at = ?, updated_at = ? WHERE id = ?`,
						resolution, now, now, c.confID,
					)
				}
			}
		}
	}
}
