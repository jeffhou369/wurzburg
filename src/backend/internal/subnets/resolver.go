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

// BuildSnapshot 從資料庫一次性載入網段、閘道與排除範圍至記憶體。
func (r *Resolver) BuildSnapshot(ctx context.Context) (*Snapshot, error) {
	if r.db == nil {
		return nil, errors.New("資料庫未連線")
	}

	snap := &Snapshot{
		gateways:   make(map[string]int64),
		exclusions: make(map[int64][]exclusionEntry),
	}

	// 1. 載入閘道位址
	gwRows, err := r.db.QueryContext(ctx, `SELECT ip_address, subnet_id FROM subnet_gateways`)
	if err != nil {
		return nil, fmt.Errorf("載入閘道位址失敗: %w", err)
	}
	defer gwRows.Close()
	for gwRows.Next() {
		var ip string
		var subID int64
		if gwRows.Scan(&ip, &subID) == nil {
			snap.gateways[ip] = subID
		}
	}

	// 2. 載入網段
	subRows, err := r.db.QueryContext(ctx, `SELECT id, cidr, state FROM subnets`)
	if err != nil {
		return nil, fmt.Errorf("載入網段失敗: %w", err)
	}
	defer subRows.Close()
	for subRows.Next() {
		var id int64
		var cidr, state string
		if subRows.Scan(&id, &cidr, &state) == nil {
			_, ipNet, err := net.ParseCIDR(cidr)
			if err == nil {
				ml, _ := ipNet.Mask.Size()
				snap.subnets = append(snap.subnets, subnetEntry{
					id: id, cidr: cidr, state: state, ipNet: ipNet, maskLen: ml,
				})
			}
		}
	}

	// 3. 載入排除範圍
	exRows, err := r.db.QueryContext(ctx, `SELECT subnet_id, start_ip, end_ip, reason FROM exclusion_ranges`)
	if err != nil {
		return nil, fmt.Errorf("載入排除範圍失敗: %w", err)
	}
	defer exRows.Close()
	for exRows.Next() {
		var subID int64
		var sStr, eStr, reason string
		if exRows.Scan(&subID, &sStr, &eStr, &reason) == nil {
			sIP := net.ParseIP(sStr).To4()
			eIP := net.ParseIP(eStr).To4()
			if sIP != nil && eIP != nil {
				snap.exclusions[subID] = append(snap.exclusions[subID], exclusionEntry{
					startUint: binary.BigEndian.Uint32(sIP),
					endUint:   binary.BigEndian.Uint32(eIP),
					reason:    reason,
				})
			}
		}
	}

	return snap, nil
}

// Resolve 解析指定 IPv4 位址之歸屬。
func (r *Resolver) Resolve(ctx context.Context, ipStr string) (IPResolutionResult, error) {
	snap, err := r.BuildSnapshot(ctx)
	if err != nil {
		return IPResolutionResult{}, err
	}
	return snap.ResolveIP(ipStr)
}
