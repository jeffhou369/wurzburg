package subnets

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strconv"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/sqlutil"
)

// affectedRecord 是範圍變更會刪除的一筆 IP 紀錄；欄位就是 token 綁定與稽核明細所需的全部內容。
type affectedRecord struct {
	id            int64
	ip            string
	mac           sql.NullString
	ownerID       sql.NullInt64
	ownerUsername string
	ownerMail     sql.NullString
	expiresAt     sql.NullTime
}

// loadAffectedRecords 讀取網段內符合 match 的 IP 紀錄（依 id 排序）。lock 為 true 時以 FOR UPDATE 鎖住
// 該網段全部紀錄（讀取最新已提交內容，且與 purge、對帳以相同的 id 遞增次序加鎖）。
// 任何查詢、Scan、迭代錯誤，以及無法解析的 IP 都會回傳錯誤，不會被當成「沒有受影響的紀錄」。
func loadAffectedRecords(ctx context.Context, tx *sql.Tx, subnetID int64, lock bool, match func(ip4 net.IP) bool) ([]affectedRecord, error) {
	query := `SELECT id, ip_address, mac_address, owner_user_id, owner_mail, expires_at
	          FROM ip_records WHERE subnet_id = ? ORDER BY id`
	if lock {
		query += ` FOR UPDATE`
	}
	rows, err := tx.QueryContext(ctx, query, subnetID)
	if err != nil {
		return nil, fmt.Errorf("查詢 IP 紀錄失敗: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []affectedRecord
	for rows.Next() {
		var rec affectedRecord
		if err := rows.Scan(&rec.id, &rec.ip, &rec.mac, &rec.ownerID, &rec.ownerMail, &rec.expiresAt); err != nil {
			return nil, fmt.Errorf("讀取 IP 紀錄失敗: %w", err)
		}
		ip4 := net.ParseIP(rec.ip).To4()
		if ip4 == nil {
			return nil, fmt.Errorf("IP 紀錄 %d 的位址 %q 不是有效的 IPv4", rec.id, rec.ip)
		}
		if match(ip4) {
			out = append(out, rec)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("迭代 IP 紀錄失敗: %w", err)
	}
	// 同一交易的連線必須先把結果讀完並關閉，才能再下查詢。
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("關閉 IP 紀錄結果集失敗: %w", err)
	}

	if err := fillOwnerUsernames(ctx, tx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// fillOwnerUsernames 以獨立查詢補上擁有者帳號（不 JOIN、不鎖 users）。
func fillOwnerUsernames(ctx context.Context, tx *sql.Tx, recs []affectedRecord) error {
	ids := make([]int64, 0)
	seen := make(map[int64]bool)
	for _, rec := range recs {
		if rec.ownerID.Valid && !seen[rec.ownerID.Int64] {
			seen[rec.ownerID.Int64] = true
			ids = append(ids, rec.ownerID.Int64)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	names := make(map[int64]string, len(ids))
	for start := 0; start < len(ids); start += scopeDeleteChunk {
		end := min(start+scopeDeleteChunk, len(ids))
		chunk := ids[start:end]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := tx.QueryContext(ctx,
			`SELECT id, username FROM users WHERE id IN (`+sqlutil.Placeholders(len(chunk))+`)`, args...)
		if err != nil {
			return fmt.Errorf("查詢擁有者帳號失敗: %w", err)
		}
		for rows.Next() {
			var id int64
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				_ = rows.Close()
				return fmt.Errorf("讀取擁有者帳號失敗: %w", err)
			}
			names[id] = name
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("迭代擁有者帳號失敗: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("關閉擁有者帳號結果集失敗: %w", err)
		}
	}
	for i := range recs {
		if recs[i].ownerID.Valid {
			recs[i].ownerUsername = names[recs[i].ownerID.Int64]
		}
	}
	return nil
}

func quotedNullString(ns sql.NullString) string {
	if !ns.Valid {
		return "-"
	}
	return strconv.Quote(ns.String)
}

// fingerprintRecords 對「逐筆內容」計算指紋：依 id 排序，涵蓋 ID、IP、MAC、擁有者（帳號 ID 與名稱）、
// 擁有者信箱與到期日。任何一筆被換掉、改擁有者、改信箱、改 MAC、改到期日，指紋都會不同；
// 因此 token 綁的是內容而不只是筆數。
func fingerprintRecords(recs []affectedRecord) string {
	sorted := make([]affectedRecord, len(recs))
	copy(sorted, recs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].id < sorted[j].id })

	h := sha256.New()
	for _, r := range sorted {
		owner := "-"
		if r.ownerID.Valid {
			owner = strconv.FormatInt(r.ownerID.Int64, 10)
		}
		expires := "-"
		if r.expiresAt.Valid {
			expires = r.expiresAt.Time.UTC().Format(time.RFC3339Nano)
		}
		_, _ = fmt.Fprintf(h, "%d|%s|%s|%s|%s|%s|%s\n",
			r.id, strconv.Quote(r.ip), quotedNullString(r.mac), owner,
			strconv.Quote(r.ownerUsername), quotedNullString(r.ownerMail), expires)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// 操作描述：token 同時綁定「做什麼」與「刪哪些」。操作描述含目標網段目前的 CIDR（與狀態），
// 因此預覽後網段本身被改動，token 也會失效。
func exclusionOpDesc(subnetID int64, cidr, startIP, endIP, reason string) string {
	return fmt.Sprintf("excl|%d|%s|%s|%s|%s", subnetID, cidr, startIP, endIP, reason)
}

func stateOpDesc(subnetID int64, cidr, fromState, toState string) string {
	return fmt.Sprintf("state|%d|%s|%s|%s", subnetID, cidr, fromState, toState)
}

func cidrOpDesc(subnetID int64, fromCIDR, toCIDR string) string {
	return fmt.Sprintf("cidr|%d|%s|%s", subnetID, fromCIDR, toCIDR)
}

func (s *ScopeService) signPreviewToken(opDesc, fingerprint string) string {
	mac := hmac.New(sha256.New, s.previewKey)
	mac.Write([]byte(previewTokenDomain))
	mac.Write([]byte{0})
	mac.Write([]byte(opDesc))
	mac.Write([]byte{0})
	mac.Write([]byte(fingerprint))
	return hex.EncodeToString(mac.Sum(nil))
}

// previewTokenFor 回傳該操作與受影響紀錄對應的 token；沒有受影響紀錄時不需要確認，token 為空字串。
func (s *ScopeService) previewTokenFor(opDesc string, recs []affectedRecord) string {
	if len(recs) == 0 {
		return ""
	}
	return s.signPreviewToken(opDesc, fingerprintRecords(recs))
}

// previewTokenMatches 以常數時間比對 token。受影響集合為空時只接受空 token。
func (s *ScopeService) previewTokenMatches(opDesc string, recs []affectedRecord, supplied string) bool {
	expected := s.previewTokenFor(opDesc, recs)
	if expected == "" {
		return supplied == ""
	}
	return hmac.Equal([]byte(expected), []byte(supplied))
}

func countOwned(recs []affectedRecord) int {
	n := 0
	for _, r := range recs {
		if r.ownerID.Valid {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// 刪除與稽核（皆在呼叫端的交易內）
// ---------------------------------------------------------------------------

type deletedOwnedDetail struct {
	IPAddress  string `json:"ip_address"`
	MACAddress string `json:"mac_address"`
	OwnerUser  string `json:"owner_username"`
	OwnerMail  string `json:"owner_mail"`
	ExpiresAt  any    `json:"expires_at"`
}

func ownedDetails(recs []affectedRecord) []deletedOwnedDetail {
	out := make([]deletedOwnedDetail, 0)
	for _, r := range recs {
		if !r.ownerID.Valid {
			continue
		}
		var expires any
		if r.expiresAt.Valid {
			expires = r.expiresAt.Time
		}
		out = append(out, deletedOwnedDetail{
			IPAddress:  r.ip,
			MACAddress: r.mac.String,
			OwnerUser:  r.ownerUsername,
			OwnerMail:  r.ownerMail.String,
			ExpiresAt:  expires,
		})
	}
	return out
}
