package testutil

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
)

// Seeder 提供自動化測試在 MariaDB 中快速佈署測試前置資料的工具。
type Seeder struct {
	DB    *sql.DB
	T     *testing.T
	Clock clock.Clock
}

// NewSeeder 建立並回傳一個 Seeder 實例。
func NewSeeder(t *testing.T, db *sql.DB, clk clock.Clock) *Seeder {
	t.Helper()
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &Seeder{DB: db, T: t, Clock: clk}
}

// SeedUser 在資料庫中建立一個系統管理員或一般使用者帳號，回傳 user_id。
func (s *Seeder) SeedUser(username, role, authSource, mail, displayName string) int64 {
	s.T.Helper()
	now := s.Clock.Now()
	if displayName == "" {
		displayName = username
	}
	res, err := s.DB.Exec(
		`INSERT INTO users (username, display_name, mail, role, auth_source, password_hash, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, '', TRUE, ?, ?)`,
		username, displayName, mail, role, authSource, now, now,
	)
	if err != nil {
		s.T.Fatalf("建立測試使用者失敗 (%s): %v", username, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// SeedAdmin 快速建立一位本機系統管理員。
func (s *Seeder) SeedAdmin(username string) int64 {
	return s.SeedUser(username, "admin", "local", "", "系統管理員")
}

// SeedSwitch 建立一台 ARP 來源交換器與其預設全域 VRF 讀取設定，回傳 (switchID, defaultVrfConfigID)。
func (s *Seeder) SeedSwitch(name, mgmtIP, platform, snmpVersion string) (int64, int64) {
	s.T.Helper()
	now := s.Clock.Now()
	res, err := s.DB.Exec(
		`INSERT INTO switches (name, mgmt_ip, platform, snmp_version, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, TRUE, ?, ?)`,
		name, mgmtIP, platform, snmpVersion, now, now,
	)
	if err != nil {
		s.T.Fatalf("建立測試交換器失敗 (%s): %v", name, err)
	}
	swID, _ := res.LastInsertId()

	resVrf, err := s.DB.Exec(
		`INSERT INTO vrf_read_configs (switch_id, vrf_name, snmp_context, is_default, enabled, created_at, updated_at)
		 VALUES (?, 'default', '', TRUE, TRUE, ?, ?)`,
		swID, now, now,
	)
	if err != nil {
		s.T.Fatalf("建立交換器預設 VRF 讀取設定失敗: %v", err)
	}
	vrfID, _ := resVrf.LastInsertId()
	return swID, vrfID
}

// SeedVRFConfig 為交換器額外建立一筆非全域 VRF 讀取設定。
func (s *Seeder) SeedVRFConfig(switchID int64, vrfName, snmpContext string) int64 {
	s.T.Helper()
	now := s.Clock.Now()
	res, err := s.DB.Exec(
		`INSERT INTO vrf_read_configs (switch_id, vrf_name, snmp_context, is_default, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, FALSE, TRUE, ?, ?)`,
		switchID, vrfName, snmpContext, now, now,
	)
	if err != nil {
		s.T.Fatalf("建立 VRF 讀取設定失敗 (%s): %v", vrfName, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// SeedPollRun 建立一筆輪詢讀取紀錄。
func (s *Seeder) SeedPollRun(vrfConfigID, switchID int64, isSuccess bool, learnedCount int, errClass string) int64 {
	s.T.Helper()
	now := s.Clock.Now()
	res, err := s.DB.Exec(
		`INSERT INTO poll_runs (vrf_config_id, switch_id, started_at, finished_at, is_success, retried, learned_count, error_class, created_at)
		 VALUES (?, ?, ?, ?, ?, FALSE, ?, ?, ?)`,
		vrfConfigID, switchID, now, now, isSuccess, learnedCount, errClass, now,
	)
	if err != nil {
		s.T.Fatalf("建立讀取紀錄失敗: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// SeedSubnet 建立一個網段，回傳 subnet_id。若 state 為 unmanaged，excludeReason 必填（如 dhcp、wireless、other）。
func (s *Seeder) SeedSubnet(cidr, state string, excludeReason *string) int64 {
	s.T.Helper()
	now := s.Clock.Now()
	res, err := s.DB.Exec(
		`INSERT INTO subnets (cidr, state, exclude_reason, is_discovered, created_at, updated_at)
		 VALUES (?, ?, ?, TRUE, ?, ?)`,
		cidr, state, excludeReason, now, now,
	)
	if err != nil {
		s.T.Fatalf("建立測試網段失敗 (%s): %v", cidr, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// SeedExclusionRange 在網段下新增排除範圍。
func (s *Seeder) SeedExclusionRange(subnetID int64, startIP, endIP, reason string) int64 {
	s.T.Helper()
	now := s.Clock.Now()
	res, err := s.DB.Exec(
		`INSERT INTO exclusion_ranges (subnet_id, start_ip, end_ip, reason, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		subnetID, startIP, endIP, reason, now, now,
	)
	if err != nil {
		s.T.Fatalf("建立排除範圍失敗 (%s-%s): %v", startIP, endIP, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// SeedGateway 為網段建立一筆閘道位址紀錄。
func (s *Seeder) SeedGateway(subnetID int64, ip, ifName string, switchID int64) int64 {
	s.T.Helper()
	now := s.Clock.Now()
	res, err := s.DB.Exec(
		`INSERT INTO subnet_gateways (subnet_id, ip_address, if_index, if_name, switch_id, vrf_name, created_at)
		 VALUES (?, ?, 1, ?, ?, 'default', ?)`,
		subnetID, ip, ifName, switchID, now,
	)
	if err != nil {
		s.T.Fatalf("建立閘道位址失敗 (%s): %v", ip, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// IPRecordSeedOpts 定義建立 IP 紀錄時的選填欄位。
type IPRecordSeedOpts struct {
	Presence     string
	Registration string
	OwnerUserID  *int64
	OwnerMail    string
	ExpiresAt    *time.Time
	LastSeenAt   *time.Time
	VendorName   string
	Notes        string
}

// SeedIPRecord 建立一筆 IP 紀錄，回傳 ip_record_id。
func (s *Seeder) SeedIPRecord(ip, mac string, subnetID int64, opts *IPRecordSeedOpts) int64 {
	s.T.Helper()
	now := s.Clock.Now()
	presence := "online"
	registration := "unregistered"
	vendor := "未知"
	var ownerID *int64
	var ownerMail, notes string
	var expiresAt *time.Time
	lastSeen := now

	if opts != nil {
		if opts.Presence != "" {
			presence = opts.Presence
		}
		if opts.Registration != "" {
			registration = opts.Registration
		}
		if opts.VendorName != "" {
			vendor = opts.VendorName
		}
		ownerID = opts.OwnerUserID
		ownerMail = opts.OwnerMail
		expiresAt = opts.ExpiresAt
		notes = opts.Notes
		if opts.LastSeenAt != nil {
			lastSeen = *opts.LastSeenAt
		}
	}

	var confirmedAt, registeredAt *time.Time
	if registration == "registered" || registration == "expired" {
		confirmedAt = &now
		registeredAt = &now
	}

	res, err := s.DB.Exec(
		`INSERT INTO ip_records (
			ip_address, mac_address, subnet_id, presence, registration,
			expires_at, confirmed_at, registered_at, first_seen_at, last_seen_at,
			owner_user_id, owner_mail, vendor_name, notes, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ip, mac, subnetID, presence, registration,
		expiresAt, confirmedAt, registeredAt, now, lastSeen,
		ownerID, ownerMail, vendor, notes, now, now,
	)
	if err != nil {
		s.T.Fatalf("建立 IP 紀錄失敗 (%s): %v", ip, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// SeedSighting 建立一筆目前有效之 ARP 觀測。
func (s *Seeder) SeedSighting(ipRecordID, vrfConfigID, switchID int64, mac, ifName string, missedRounds int) int64 {
	s.T.Helper()
	now := s.Clock.Now()
	res, err := s.DB.Exec(
		`INSERT INTO arp_sightings (ip_record_id, vrf_config_id, switch_id, vrf_name, mac_address, if_name, arp_type, last_seen_at, missed_rounds, created_at, updated_at)
		 VALUES (?, ?, ?, 'default', ?, ?, 'dynamic', ?, ?, ?, ?)`,
		ipRecordID, vrfConfigID, switchID, mac, ifName, now, missedRounds, now, now,
	)
	if err != nil {
		s.T.Fatalf("建立 ARP 觀測失敗: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// SeedIPEvent 建立一筆 IP 變更事件。
func (s *Seeder) SeedIPEvent(ipRecordID int64, ip, eventType, details string) int64 {
	s.T.Helper()
	now := s.Clock.Now()
	res, err := s.DB.Exec(
		`INSERT INTO ip_events (ip_record_id, ip_address, event_type, details, occurred_at)
		 VALUES (?, ?, ?, ?, ?)`,
		ipRecordID, ip, eventType, details, now,
	)
	if err != nil {
		s.T.Fatalf("建立 IP 變更事件失敗: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// SeedConflict 建立一筆 MAC 衝突紀錄。
func (s *Seeder) SeedConflict(ipRecordID int64, ip, adoptedMAC string, conflictMACs []string, status string) int64 {
	s.T.Helper()
	now := s.Clock.Now()
	macJSON, err := json.Marshal(conflictMACs)
	if err != nil {
		s.T.Fatalf("序列化衝突 MAC 清單失敗: %v", err)
	}
	res, err := s.DB.Exec(
		`INSERT INTO ip_conflicts (ip_record_id, ip_address, adopted_mac, conflict_macs, status, first_detected_at, last_detected_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ipRecordID, ip, adoptedMAC, string(macJSON), status, now, now, now, now,
	)
	if err != nil {
		s.T.Fatalf("建立衝突紀錄失敗 (%s): %v", ip, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// SeedNotificationState 建立一筆通知去重狀態紀錄。
func (s *Seeder) SeedNotificationState(dedupKey, notifType string, isNotified bool) int64 {
	s.T.Helper()
	now := s.Clock.Now()
	res, err := s.DB.Exec(
		`INSERT INTO notification_states (dedup_key, notif_type, is_notified, notified_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		dedupKey, notifType, isNotified, now, now,
	)
	if err != nil {
		s.T.Fatalf("建立通知狀態失敗 (%s): %v", dedupKey, err)
	}
	id, _ := res.LastInsertId()
	return id
}
