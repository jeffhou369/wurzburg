package subnets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"

	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
)

// ApplyInterfaceAddresses 實作 pipeline.SubnetScopeProcessor，從採集到的介面位址表發現網段與閘道位址。
func (s *ScopeService) ApplyInterfaceAddresses(ctx context.Context, outputs []*interfaces.PollOutput) error {
	if s.db == nil || len(outputs) == 0 {
		return nil
	}

	now := s.clock.Now()
	seenCIDRs := make(map[string]bool)

	for _, out := range outputs {
		if out == nil {
			continue
		}
		for _, addr := range out.InterfaceAddresses {
			cidr, err := toNetworkCIDR(addr.IPAddress, addr.NetMask)
			if err != nil {
				continue
			}
			seenCIDRs[cidr] = true

			// 尋找或建立網段（新發現者預設為 pending 待檢視）
			var subnetID int64
			err = s.db.QueryRowContext(ctx, `SELECT id FROM subnets WHERE cidr = ?`, cidr).Scan(&subnetID)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				res, insErr := s.db.ExecContext(
					ctx,
					`INSERT INTO subnets (cidr, state, is_discovered, source_missing, created_at, updated_at)
					 VALUES (?, 'pending', TRUE, FALSE, ?, ?)`,
					cidr, now, now,
				)
				if insErr != nil {
					return fmt.Errorf("新增發現網段 %s 失敗: %w", cidr, insErr)
				}
				if subnetID, err = res.LastInsertId(); err != nil {
					return fmt.Errorf("取得發現網段 %s 編號失敗: %w", cidr, err)
				}
			case err != nil:
				return fmt.Errorf("查詢網段 %s 失敗: %w", cidr, err)
			default:
				if _, err := s.db.ExecContext(ctx, `UPDATE subnets SET source_missing = FALSE, updated_at = ? WHERE id = ?`, now, subnetID); err != nil {
					return fmt.Errorf("更新網段 %s 來源狀態失敗: %w", cidr, err)
				}
			}

			if subnetID > 0 {
				if _, err := s.db.ExecContext(
					ctx,
					`INSERT INTO subnet_gateways (subnet_id, ip_address, if_index, if_name, switch_id, vrf_name, created_at)
					 VALUES (?, ?, ?, ?, ?, ?, ?)
					 ON DUPLICATE KEY UPDATE if_index = VALUES(if_index), if_name = VALUES(if_name)`,
					subnetID, addr.IPAddress, addr.IfIndex, addr.IfName, out.SwitchID, out.VRFName, now,
				); err != nil {
					return fmt.Errorf("寫入閘道位址 %s 失敗: %w", addr.IPAddress, err)
				}
			}
		}
	}

	// 標記來源已消失的發現網段（僅在本次確實有成功的讀取輸出時比對）
	if len(seenCIDRs) > 0 {
		rows, err := s.db.QueryContext(ctx, `SELECT id, cidr FROM subnets WHERE is_discovered = TRUE AND source_missing = FALSE`)
		if err != nil {
			return fmt.Errorf("查詢發現網段失敗: %w", err)
		}
		var missingIDs []int64
		for rows.Next() {
			var id int64
			var cidr string
			if err := rows.Scan(&id, &cidr); err != nil {
				_ = rows.Close()
				return fmt.Errorf("讀取發現網段失敗: %w", err)
			}
			if !seenCIDRs[cidr] {
				missingIDs = append(missingIDs, id)
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("讀取發現網段失敗: %w", err)
		}
		_ = rows.Close()
		for _, id := range missingIDs {
			if _, err := s.db.ExecContext(ctx, `UPDATE subnets SET source_missing = TRUE, updated_at = ? WHERE id = ?`, now, id); err != nil {
				return fmt.Errorf("標記網段來源消失失敗: %w", err)
			}
		}
	}

	return nil
}

func toNetworkCIDR(ipStr, maskStr string) (string, error) {
	ip := net.ParseIP(ipStr).To4()
	if ip == nil {
		return "", errors.New("invalid IPv4")
	}
	maskIP := net.ParseIP(maskStr).To4()
	if maskIP == nil {
		return "", errors.New("invalid NetMask")
	}
	mask := net.IPv4Mask(maskIP[0], maskIP[1], maskIP[2], maskIP[3])
	ones, bits := mask.Size()
	if bits != 32 || (ones == 0 && maskStr != "0.0.0.0") {
		return "", errors.New("non-contiguous mask")
	}
	if ones == 32 {
		return "", errors.New("ignore /32 host loopback")
	}
	ipNet := &net.IPNet{IP: ip.Mask(mask), Mask: mask}
	return ipNet.String(), nil
}

// ---------------------------------------------------------------------------
// 錯誤表達
// ---------------------------------------------------------------------------
