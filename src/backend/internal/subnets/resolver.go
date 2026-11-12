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

// Resolver 負責查詢 IPv4 在系統中的網段歸屬與管理範圍資格。
type Resolver struct {
	db *sql.DB
}

func NewResolver(db *sql.DB) *Resolver {
	return &Resolver{db: db}
}

// Resolve 解析指定 IPv4 位址之歸屬。
func (r *Resolver) Resolve(ctx context.Context, ipStr string) (IPResolutionResult, error) {
	parsedIP := net.ParseIP(ipStr).To4()
	if parsedIP == nil {
		return IPResolutionResult{}, fmt.Errorf("無效的 IPv4 位址: %s", ipStr)
	}
	ipUint := binary.BigEndian.Uint32(parsedIP)

	if r.db == nil {
		return IPResolutionResult{}, errors.New("資料庫未連線")
	}

	// 1. 檢查是否為交換器自身之介面閘道位址 (subnet_gateways)
	var gwSubnetID int64
	err := r.db.QueryRowContext(
		ctx,
		`SELECT subnet_id FROM subnet_gateways WHERE ip_address = ? LIMIT 1`,
		ipStr,
	).Scan(&gwSubnetID)
	if err == nil {
		return IPResolutionResult{
			SubnetID:         gwSubnetID,
			IsGateway:        true,
			ShouldHaveRecord: false,
		}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return IPResolutionResult{}, fmt.Errorf("查詢閘道位址失敗: %w", err)
	}

	// 2. 查詢所有網段並尋找匹配且前綴最長者（Longest Prefix Match）
	rows, err := r.db.QueryContext(ctx, `SELECT id, cidr, state FROM subnets`)
	if err != nil {
		return IPResolutionResult{}, fmt.Errorf("查詢網段列表失敗: %w", err)
	}
	defer rows.Close()

	var matchedID int64
	var matchedCIDR, matchedState string
	maxMaskLen := -1

	for rows.Next() {
		var sID int64
		var sCIDR, sState string
		if err := rows.Scan(&sID, &sCIDR, &sState); err != nil {
			continue
		}

		_, ipNet, parseErr := net.ParseCIDR(sCIDR)
		if parseErr != nil {
			continue
		}

		if ipNet.Contains(parsedIP) {
			maskLen, _ := ipNet.Mask.Size()
			if maskLen > maxMaskLen {
				maxMaskLen = maskLen
				matchedID = sID
				matchedCIDR = sCIDR
				matchedState = sState
			}
		}
	}

	if maxMaskLen == -1 {
		// 不屬於任何已知網段（未定前綴）
		return IPResolutionResult{
			ShouldHaveRecord: false,
		}, nil
	}

	// 3. 檢查是否落在該網段的排除範圍中
	exRows, err := r.db.QueryContext(
		ctx,
		`SELECT start_ip, end_ip, reason FROM exclusion_ranges WHERE subnet_id = ?`,
		matchedID,
	)
	if err != nil {
		return IPResolutionResult{}, fmt.Errorf("查詢排除範圍失敗: %w", err)
	}
	defer exRows.Close()

	for exRows.Next() {
		var startStr, endStr, reason string
		if err := exRows.Scan(&startStr, &endStr, &reason); err != nil {
			continue
		}

		sIP := net.ParseIP(startStr).To4()
		eIP := net.ParseIP(endStr).To4()
		if sIP == nil || eIP == nil {
			continue
		}

		startUint := binary.BigEndian.Uint32(sIP)
		endUint := binary.BigEndian.Uint32(eIP)

		if ipUint >= startUint && ipUint <= endUint {
			return IPResolutionResult{
				SubnetID:         matchedID,
				SubnetCIDR:       matchedCIDR,
				SubnetState:      matchedState,
				IsExcluded:       true,
				ExcludeReason:    reason,
				ShouldHaveRecord: false,
			}, nil
		}
	}

	// 僅有在網段狀態為 managed 且未被排除、非閘道位址時，才應存在 IP 紀錄
	shouldHave := matchedState == "managed"

	return IPResolutionResult{
		SubnetID:         matchedID,
		SubnetCIDR:       matchedCIDR,
		SubnetState:      matchedState,
		ShouldHaveRecord: shouldHave,
	}, nil
}
