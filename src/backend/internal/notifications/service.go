package notifications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/audit"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/mailer"
	"github.com/jeffhou369/wurzburg/backend/internal/settings"
)

// Service 負責執行三種業務通知信的排程工作：未登記在線彙整、未登記累積總表與到期催認。
type Service struct {
	db       *sql.DB
	clock    clock.Clock
	settings *settings.Store
	audit    *audit.Logger
	mailer   interfaces.Mailer
}

func NewService(db *sql.DB, clk clock.Clock, sett *settings.Store, aud *audit.Logger, m interfaces.Mailer) *Service {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &Service{
		db:       db,
		clock:    clk,
		settings: sett,
		audit:    aud,
		mailer:   m,
	}
}

func (s *Service) SetMailer(m interfaces.Mailer) {
	s.mailer = m
}

// 輔助函式：取得系統設定之時區 Location 與名稱。
func (s *Service) getLocation(ctx context.Context) (*time.Location, string) {
	tzName := "Asia/Taipei"
	if s.settings != nil {
		if val, err := s.settings.Get(ctx, "timezone"); err == nil && val != "" {
			tzName = val
		}
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.FixedZone("Asia/Taipei", 8*3600)
	}
	return loc, tzName
}

// 輔助函式：取得園區名稱。
func (s *Service) getCampusName(ctx context.Context) string {
	campus := "Würzburg"
	if s.settings != nil {
		if val, err := s.settings.Get(ctx, "campus_name"); err == nil && val != "" {
			campus = val
		}
	}
	return campus
}

// 輔助函式：取得系統管理員通知收件信箱清單。
func (s *Service) getAdminRecipients(ctx context.Context) []string {
	if s.settings == nil {
		return nil
	}
	raw, err := s.settings.Get(ctx, "admin_recipients")
	if err != nil || raw == "" {
		return nil
	}
	var rcpts []string
	_ = json.Unmarshal([]byte(raw), &rcpts)
	var clean []string
	for _, r := range rcpts {
		trimmed := strings.TrimSpace(r)
		if trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	return clean
}

// =========================================================================
// 1. 未登記在線位址彙整 (Unregistered Digest - 每小時執行)
// =========================================================================

type unregRecordItem struct {
	ID          int64
	IPAddress   string
	MACAddress  string
	VendorName  string
	SubnetCIDR  string
	SwitchNames string
	IfNames     string
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

// RunUnregisteredDigest 每小時執行：比對在線且未登記之 IP 紀錄，找出去重鍵尚未標示已通知的新增項目並寄送彙整。
func (s *Service) RunUnregisteredDigest(ctx context.Context) (int, error) {
	if s.db == nil || s.mailer == nil {
		return 0, nil
	}

	loc, tzName := s.getLocation(ctx)
	campusName := s.getCampusName(ctx)
	adminRecipients := s.getAdminRecipients(ctx)
	if len(adminRecipients) == 0 {
		return 0, errors.New("尚未設定系統管理員通知收件清單，略過未登記彙整寄送")
	}

	// 1. 查詢所有在線且未登記之 IP 紀錄與其關聯之交換器/介面資訊
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT r.id, r.ip_address, r.mac_address, r.vendor_name, sub.cidr,
		        COALESCE((SELECT GROUP_CONCAT(DISTINCT sw.name) FROM arp_sightings s JOIN switches sw ON s.switch_id = sw.id WHERE s.ip_record_id = r.id), ''),
		        COALESCE((SELECT GROUP_CONCAT(DISTINCT s.if_name) FROM arp_sightings s WHERE s.ip_record_id = r.id AND s.if_name != ''), ''),
		        r.first_seen_at, r.last_seen_at
		 FROM ip_records r
		 JOIN subnets sub ON r.subnet_id = sub.id
		 WHERE r.presence = 'online' AND r.registration = 'unregistered'
		 ORDER BY INET_ATON(r.ip_address) ASC`,
	)
	if err != nil {
		return 0, fmt.Errorf("查詢在線未登記 IP 紀錄失敗: %w", err)
	}
	defer rows.Close()

	var allOnlineUnreg []unregRecordItem
	for rows.Next() {
		var it unregRecordItem
		if err := rows.Scan(&it.ID, &it.IPAddress, &it.MACAddress, &it.VendorName, &it.SubnetCIDR, &it.SwitchNames, &it.IfNames, &it.FirstSeenAt, &it.LastSeenAt); err == nil {
			allOnlineUnreg = append(allOnlineUnreg, it)
		}
	}

	if len(allOnlineUnreg) == 0 {
		return 0, nil
	}

	// 2. 查詢目前已處於 notified 狀態的去重鍵集合
	stateRows, err := s.db.QueryContext(ctx, `SELECT dedup_key FROM notification_states WHERE notif_type = 'unregistered_digest' AND is_notified = TRUE`)
	if err != nil {
		return 0, fmt.Errorf("查詢通知去重狀態失敗: %w", err)
	}
	defer stateRows.Close()

	notifiedKeys := make(map[string]bool)
	for stateRows.Next() {
		var k string
		if err := stateRows.Scan(&k); err == nil {
			notifiedKeys[k] = true
		}
	}

	var newItems []unregRecordItem
	cumulativeCount := 0
	for _, it := range allOnlineUnreg {
		dKey := fmt.Sprintf("unreg|%s|%s", it.IPAddress, it.MACAddress)
		if notifiedKeys[dKey] {
			cumulativeCount++
		} else {
			newItems = append(newItems, it)
		}
	}

	// 若無任何新項目，依規格不寄送
	if len(newItems) == 0 {
		return 0, nil
	}

	// 計算涉及網段數與交換器數
	subnetSet := make(map[string]bool)
	switchSet := make(map[string]bool)
	for _, it := range newItems {
		subnetSet[it.SubnetCIDR] = true
		for _, sw := range strings.Split(it.SwitchNames, ",") {
			sw = strings.TrimSpace(sw)
			if sw != "" {
				switchSet[sw] = true
			}
		}
	}

	now := s.clock.Now()
	nowLocalStr := now.In(loc).Format("2006-01-02 15:04") + " " + tzName

	maxItems := 50
	if s.settings != nil {
		maxItems = s.settings.GetInt(ctx, "unreg_digest_max_items", 50)
	}

	displayRows := make([][]mailer.EmailCell, 0, len(newItems))
	for i, it := range newItems {
		if i >= maxItems {
			break
		}
		displayRows = append(displayRows, []mailer.EmailCell{
			{Text: "[在線 · 未登記]", BadgeTone: "warning"},
			{Text: it.IPAddress, Monospace: true},
			{Text: it.MACAddress, Monospace: true},
			{Text: it.VendorName},
			{Text: it.SubnetCIDR, Monospace: true},
			{Text: it.SwitchNames},
			{Text: it.IfNames},
			{Text: it.FirstSeenAt.In(loc).Format("2006-01-02 15:04")},
			{Text: it.LastSeenAt.In(loc).Format("2006-01-02 15:04")},
		})
	}

	truncNote := ""
	if len(newItems) > maxItems {
		truncNote = fmt.Sprintf("本次新增共 %d 筆，信件僅列出前 %d 筆，其餘 %d 筆請登入系統檢視完整清單。", len(newItems), maxItems, len(newItems)-maxItems)
	}

	tplData := mailer.EmailTemplateData{
		CampusName: campusName,
		Subject:    fmt.Sprintf("[%s] 未登記在線位址彙整（新增 %d 筆，%s）", campusName, len(newItems), nowLocalStr),
		Preheader:  fmt.Sprintf("新增 %d 筆未登記在線位址待處理", len(newItems)),
		Greeting:   "系統管理員您好：",
		Intro:      "系統於納管網段內偵測到新出現且尚未登記負責人之在線 IP 位址，彙整如下：",
		Timezone:   tzName,
		SummaryItems: []mailer.SummaryKV{
			{Label: "新增待處理", Value: fmt.Sprintf("%d 筆", len(newItems)), Tone: "warning"},
			{Label: "累計仍未登記", Value: fmt.Sprintf("%d 筆", cumulativeCount), Tone: "default"},
			{Label: "涉及網段數", Value: fmt.Sprintf("%d 個", len(subnetSet)), Tone: "default"},
			{Label: "涉及交換器數", Value: fmt.Sprintf("%d 台", len(switchSet)), Tone: "default"},
		},
		Columns:       []string{"狀態", "IP 位址", "MAC 位址", "廠商", "網段", "交換器", "介面", "首次見到", "最近見到"},
		Rows:          displayRows,
		TruncatedNote: truncNote,
		PrimaryCTA: &mailer.CTAButton{
			Label: "前往檢視在線未登記清單",
			URL:   "http://localhost:8080/?presence=online&registration=unregistered",
		},
		FooterLines: []string{
			"您之所以收到此信，是因為您的信箱位於「系統管理員收件清單」中。",
			"本彙整每小時執行一次；已通知過之位址在完成登記或由管理員手動重設前不會重複列為新增。",
		},
	}

	htmlBody, textBody, err := mailer.RenderEmail(tplData)
	if err != nil {
		return 0, fmt.Errorf("產生未登記彙整郵件本文失敗: %w", err)
	}

	mailMsg := interfaces.MailMessage{
		To:       adminRecipients,
		Subject:  tplData.Subject,
		TextBody: textBody,
		HTMLBody: htmlBody,
	}

	// 嘗試寄送
	var sendErr error
	if sm, ok := s.mailer.(*mailer.SMTPMailer); ok {
		_, sendErr = sm.SendWithTracking(ctx, "unregistered_digest", "", mailMsg)
	} else {
		sendErr = s.mailer.Send(ctx, mailMsg)
		s.recordDirectLog(ctx, "unregistered_digest", "", adminRecipients, nil, tplData.Subject, sendErr)
	}

	if sendErr != nil {
		return 0, fmt.Errorf("寄送未登記彙整郵件失敗: %w", sendErr)
	}

	// 寄送成功：將本次新增項目的去重鍵標記為已通知
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return len(newItems), err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(
		ctx,
		`INSERT INTO notification_states (dedup_key, notif_type, is_notified, notified_at, updated_at)
		 VALUES (?, 'unregistered_digest', TRUE, ?, ?)
		 ON DUPLICATE KEY UPDATE is_notified = TRUE, notified_at = VALUES(notified_at), updated_at = VALUES(updated_at)`,
	)
	if err != nil {
		return len(newItems), err
	}
	defer stmt.Close()

	for _, it := range newItems {
		dKey := fmt.Sprintf("unreg|%s|%s", it.IPAddress, it.MACAddress)
		if _, err := stmt.ExecContext(ctx, dKey, now, now); err != nil {
			return len(newItems), err
		}
	}

	if err := tx.Commit(); err != nil {
		return len(newItems), err
	}

	return len(newItems), nil
}

// =========================================================================
// 2. 仍未登記在線位址每週總表 (Weekly Summary - 每週一 09:00 執行)
// =========================================================================

type weeklyRecordItem struct {
	ID          int64
	IPAddress   string
	MACAddress  string
	VendorName  string
	SubnetCIDR  string
	SwitchNames string
	IfNames     string
	FirstSeenAt time.Time
	NotifiedAt  time.Time
}

// RunWeeklySummary 每週執行一次：將所有已被彙整通知過、目前仍在線且尚未登記之位址列為累積總表。
func (s *Service) RunWeeklySummary(ctx context.Context) (int, error) {
	if s.db == nil || s.mailer == nil {
		return 0, nil
	}

	loc, tzName := s.getLocation(ctx)
	campusName := s.getCampusName(ctx)
	adminRecipients := s.getAdminRecipients(ctx)
	if len(adminRecipients) == 0 {
		return 0, errors.New("尚未設定系統管理員通知收件清單，略過未登記總表寄送")
	}

	now := s.clock.Now()
	year, week := now.In(loc).ISOWeek()
	weekDedupKey := fmt.Sprintf("weekly|%d-W%02d", year, week)

	// 檢查同一週是否已成功寄出
	var sentCnt int
	err := s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM notification_logs WHERE notif_type = 'unregistered_weekly' AND dedup_key = ? AND status = 'sent'`,
		weekDedupKey,
	).Scan(&sentCnt)
	if err == nil && sentCnt > 0 {
		return 0, nil // 本週已寄送過，避免重複寄送
	}

	// 查詢已通知、仍在線且未登記之 IP 紀錄
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT r.id, r.ip_address, r.mac_address, r.vendor_name, sub.cidr,
		        COALESCE((SELECT GROUP_CONCAT(DISTINCT sw.name) FROM arp_sightings s JOIN switches sw ON s.switch_id = sw.id WHERE s.ip_record_id = r.id), ''),
		        COALESCE((SELECT GROUP_CONCAT(DISTINCT s.if_name) FROM arp_sightings s WHERE s.ip_record_id = r.id AND s.if_name != ''), ''),
		        r.first_seen_at, ns.notified_at
		 FROM ip_records r
		 JOIN subnets sub ON r.subnet_id = sub.id
		 JOIN notification_states ns ON ns.dedup_key = CONCAT('unreg|', r.ip_address, '|', r.mac_address)
		 WHERE r.presence = 'online' AND r.registration = 'unregistered' AND ns.notif_type = 'unregistered_digest' AND ns.is_notified = TRUE
		 ORDER BY INET_ATON(r.ip_address) ASC`,
	)
	if err != nil {
		return 0, fmt.Errorf("查詢累積未登記 IP 紀錄失敗: %w", err)
	}
	defer rows.Close()

	var items []weeklyRecordItem
	var earliestNotified time.Time
	for rows.Next() {
		var it weeklyRecordItem
		if err := rows.Scan(&it.ID, &it.IPAddress, &it.MACAddress, &it.VendorName, &it.SubnetCIDR, &it.SwitchNames, &it.IfNames, &it.FirstSeenAt, &it.NotifiedAt); err == nil {
			items = append(items, it)
			if earliestNotified.IsZero() || it.NotifiedAt.Before(earliestNotified) {
				earliestNotified = it.NotifiedAt
			}
		}
	}

	if len(items) == 0 {
		return 0, nil // 沒有累積項目則不發送信件
	}

	subnetSet := make(map[string]bool)
	switchSet := make(map[string]bool)
	for _, it := range items {
		subnetSet[it.SubnetCIDR] = true
		for _, sw := range strings.Split(it.SwitchNames, ",") {
			sw = strings.TrimSpace(sw)
			if sw != "" {
				switchSet[sw] = true
			}
		}
	}

	dateStr := now.In(loc).Format("2006-01-02") + " " + tzName
	earliestDaysAgo := int(now.Sub(earliestNotified).Hours() / 24)
	earliestText := fmt.Sprintf("%d 天前（%s）", earliestDaysAgo, earliestNotified.In(loc).Format("2006-01-02"))

	maxItems := 50
	displayRows := make([][]mailer.EmailCell, 0, len(items))
	for i, it := range items {
		if i >= maxItems {
			break
		}
		displayRows = append(displayRows, []mailer.EmailCell{
			{Text: "[在線 · 未登記]", BadgeTone: "warning"},
			{Text: it.IPAddress, Monospace: true},
			{Text: it.MACAddress, Monospace: true},
			{Text: it.VendorName},
			{Text: it.SubnetCIDR, Monospace: true},
			{Text: it.SwitchNames},
			{Text: it.IfNames},
			{Text: it.FirstSeenAt.In(loc).Format("2006-01-02 15:04")},
			{Text: it.NotifiedAt.In(loc).Format("2006-01-02 15:04")},
		})
	}

	truncNote := ""
	if len(items) > maxItems {
		truncNote = fmt.Sprintf("目前累積共 %d 筆未登記位址，信件僅列出前 %d 筆，其餘 %d 筆請登入系統檢視完整清單。", len(items), maxItems, len(items)-maxItems)
	}

	tplData := mailer.EmailTemplateData{
		CampusName: campusName,
		Subject:    fmt.Sprintf("[%s] 仍未登記的在線位址總表（共 %d 筆，%s）", campusName, len(items), dateStr),
		Preheader:  fmt.Sprintf("目前累積共 %d 筆在線位址仍未登記負責人", len(items)),
		Greeting:   "系統管理員您好：",
		Intro:      "以下為園區內曾透過彙整通知、目前仍在線但持續尚未登記負責人之 IP 位址累積總表：",
		Timezone:   tzName,
		SummaryItems: []mailer.SummaryKV{
			{Label: "仍未登記", Value: fmt.Sprintf("%d 筆", len(items)), Tone: "warning"},
			{Label: "涉及網段數", Value: fmt.Sprintf("%d 個", len(subnetSet)), Tone: "default"},
			{Label: "涉及交換器數", Value: fmt.Sprintf("%d 台", len(switchSet)), Tone: "default"},
			{Label: "最早通知記錄", Value: earliestText, Tone: "default"},
		},
		Columns:       []string{"狀態", "IP 位址", "MAC 位址", "廠商", "網段", "交換器", "介面", "首次見到", "首次通知時間"},
		Rows:          displayRows,
		TruncatedNote: truncNote,
		PrimaryCTA: &mailer.CTAButton{
			Label: "前往檢視在線未登記清單",
			URL:   "http://localhost:8080/?presence=online&registration=unregistered",
		},
		FooterLines: []string{
			"您之所以收到此信，是因為您的信箱位於「系統管理員收件清單」中。",
			"本總表每週定期寄發一次，不會改變任何現有 IP 位址的通知狀態。",
		},
	}

	htmlBody, textBody, err := mailer.RenderEmail(tplData)
	if err != nil {
		return 0, fmt.Errorf("產生未登記總表郵件本文失敗: %w", err)
	}

	mailMsg := interfaces.MailMessage{
		To:       adminRecipients,
		Subject:  tplData.Subject,
		TextBody: textBody,
		HTMLBody: htmlBody,
	}

	var sendErr error
	if sm, ok := s.mailer.(*mailer.SMTPMailer); ok {
		_, sendErr = sm.SendWithTracking(ctx, "unregistered_weekly", weekDedupKey, mailMsg)
	} else {
		sendErr = s.mailer.Send(ctx, mailMsg)
		s.recordDirectLog(ctx, "unregistered_weekly", weekDedupKey, adminRecipients, nil, tplData.Subject, sendErr)
	}

	if sendErr != nil {
		return 0, fmt.Errorf("寄送未登記總表郵件失敗: %w", sendErr)
	}

	return len(items), nil
}

// =========================================================================
// 3. 到期催認 (Expiry Reminders - 每日執行)
// =========================================================================

type dueReminderRecord struct {
	ID          int64
	IPAddress   string
	MACAddress  string
	VendorName  string
	DeviceType  string
	Notes       string
	SubnetCIDR  string
	OwnerMail   string
	Presence    string
	ExpiresAt   time.Time
	ConfirmedAt time.Time
	Round       int
	IsExpired   bool
	IsFinal     bool
	DedupKey    string
}

// RunExpiryReminders 每日執行：檢查所有即將到期與逾期未確認之登記，依時程發送催認通知並自動合併同信箱多筆。
func (s *Service) RunExpiryReminders(ctx context.Context) (int, error) {
	if s.db == nil || s.mailer == nil {
		return 0, nil
	}

	loc, tzName := s.getLocation(ctx)
	campusName := s.getCampusName(ctx)
	adminRecipients := s.getAdminRecipients(ctx)

	advanceDays := 14
	intervalDays := 7
	maxReminders := 4
	if s.settings != nil {
		advanceDays = s.settings.GetInt(ctx, "expiry_advance_days", 14)
		intervalDays = s.settings.GetInt(ctx, "expiry_reminder_interval_days", 7)
		maxReminders = s.settings.GetInt(ctx, "expiry_max_reminders", 4)
	}

	now := s.clock.Now()
	today := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), 0, 0, 0, 0, loc)

	// 查詢所有已有擁有者且具有到期日之有效登記
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT r.id, r.ip_address, r.mac_address, r.vendor_name, COALESCE(r.device_type, ''), COALESCE(r.notes, ''),
		        sub.cidr, r.owner_mail, r.presence, r.expires_at, COALESCE(r.confirmed_at, r.registered_at)
		 FROM ip_records r
		 JOIN subnets sub ON r.subnet_id = sub.id
		 WHERE r.owner_user_id IS NOT NULL AND r.owner_mail != '' AND r.expires_at IS NOT NULL
		 ORDER BY INET_ATON(r.ip_address) ASC`,
	)
	if err != nil {
		return 0, fmt.Errorf("查詢登記到期紀錄失敗: %w", err)
	}
	defer rows.Close()

	// 讀取既有到期催認之去重狀態
	stateRows, err := s.db.QueryContext(ctx, `SELECT dedup_key FROM notification_states WHERE notif_type = 'expiry_reminder' AND is_notified = TRUE`)
	if err != nil {
		return 0, fmt.Errorf("查詢到期去重狀態失敗: %w", err)
	}
	defer stateRows.Close()

	notifiedKeys := make(map[string]bool)
	for stateRows.Next() {
		var k string
		if err := stateRows.Scan(&k); err == nil {
			notifiedKeys[k] = true
		}
	}

	var candidates []dueReminderRecord
	for rows.Next() {
		var it dueReminderRecord
		var exp, conf time.Time
		if err := rows.Scan(&it.ID, &it.IPAddress, &it.MACAddress, &it.VendorName, &it.DeviceType, &it.Notes, &it.SubnetCIDR, &it.OwnerMail, &it.Presence, &exp, &conf); err != nil {
			continue
		}
		it.ExpiresAt = exp
		it.ConfirmedAt = conf

		expDay := time.Date(exp.In(loc).Year(), exp.In(loc).Month(), exp.In(loc).Day(), 0, 0, 0, 0, loc)
		diffDays := int(today.Sub(expDay).Hours() / 24)
		expDateStr := expDay.Format("2006-01-02")

		// 判定各輪次：
		// 1. 到期前 advanceDays (例如 -14 天) -> Round 1
		// 2. 到期當天 (0 天) -> Round 2
		// 3. 逾期後每 intervalDays (例如 +7, +14, +21, +28 天) -> Round 2+k
		var curRound int
		var isExpired, isFinal bool

		if diffDays >= -advanceDays && diffDays < 0 {
			curRound = 1
			isExpired = false
		} else if diffDays >= 0 && diffDays < intervalDays {
			curRound = 2
			isExpired = true
		} else if diffDays >= intervalDays {
			k := diffDays / intervalDays
			if k > maxReminders {
				continue // 催到上限後停止寄信，不自動釋放
			}
			curRound = 2 + k
			isExpired = true
			if k == maxReminders {
				isFinal = true
			}
		} else {
			continue // 尚未進入任何催認時段
		}

		dKey := fmt.Sprintf("expiry|%s|%s|%d", it.IPAddress, expDateStr, curRound)
		if notifiedKeys[dKey] {
			continue // 該輪次已寄送過
		}

		it.Round = curRound
		it.IsExpired = isExpired
		it.IsFinal = isFinal
		it.DedupKey = dKey
		candidates = append(candidates, it)
	}

	if len(candidates) == 0 {
		return 0, nil
	}

	// 依照 owner_mail 聚合並穩定排序（同一個擁有者信箱同一天合併成一封信）
	grouped := make(map[string][]dueReminderRecord)
	for _, c := range candidates {
		grouped[c.OwnerMail] = append(grouped[c.OwnerMail], c)
	}
	mailKeys := make([]string, 0, len(grouped))
	for k := range grouped {
		mailKeys = append(mailKeys, k)
	}
	sort.Strings(mailKeys)

	sentTotal := 0
	for _, mailAddr := range mailKeys {
		items := grouped[mailAddr]
		hasFinal := false
		hasExpired := false
		maxRound := 1
		var earliestExp time.Time

		for _, it := range items {
			if it.IsFinal {
				hasFinal = true
			}
			if it.IsExpired {
				hasExpired = true
			}
			if it.Round > maxRound {
				maxRound = it.Round
			}
			if earliestExp.IsZero() || it.ExpiresAt.Before(earliestExp) {
				earliestExp = it.ExpiresAt
			}
		}

		var subject string
		var intro string
		if hasExpired {
			reminderIdx := maxRound - 1
			subject = fmt.Sprintf("[%s] 請重新確認您的 IP 登記：已逾期催認（第 %d 次）", campusName, reminderIdx)
			intro = fmt.Sprintf("您所負責登記之下列 %d 筆 IP 位址已逾期，請儘速登入系統進行重新確認以延長有效期限，或釋放不再使用之位址：", len(items))
		} else {
			subject = fmt.Sprintf("[%s] 請重新確認您的 IP 登記：即將到期提醒（第 1 次）", campusName)
			intro = fmt.Sprintf("您所負責登記之下列 %d 筆 IP 位址即將到期，請登入系統檢視並重新確認以延長有效期限：", len(items))
		}

		displayRows := make([][]mailer.EmailCell, 0, len(items))
		for _, it := range items {
			stText := "[在線 · 即將到期]"
			badge := "warning"
			if it.IsExpired {
				badge = "danger"
				if it.Presence == "online" {
					stText = "[在線 · 已到期]"
				} else {
					stText = "[離線 · 已到期]"
				}
			} else if it.Presence == "offline" {
				stText = "[離線 · 即將到期]"
			}

			displayRows = append(displayRows, []mailer.EmailCell{
				{Text: it.IPAddress, Monospace: true},
				{Text: stText, BadgeTone: badge},
				{Text: it.MACAddress, Monospace: true},
				{Text: it.VendorName},
				{Text: it.SubnetCIDR, Monospace: true},
				{Text: it.DeviceType},
				{Text: it.Notes}, // 模板中會自動進行 HTML 跳脫
				{Text: it.ConfirmedAt.In(loc).Format("2006-01-02")},
				{Text: it.ExpiresAt.In(loc).Format("2006-01-02")},
			})
		}

		var cc []string
		if hasFinal && len(adminRecipients) > 0 {
			cc = adminRecipients
		}

		tplData := mailer.EmailTemplateData{
			CampusName: campusName,
			Subject:    subject,
			Preheader:  fmt.Sprintf("請確認您所負責的 %d 筆 IP 登記", len(items)),
			Greeting:   "您好：",
			Intro:      intro,
			Timezone:   tzName,
			SummaryItems: []mailer.SummaryKV{
				{Label: "需處理筆數", Value: fmt.Sprintf("%d 筆", len(items)), Tone: "warning"},
				{Label: "最早到期日", Value: earliestExp.In(loc).Format("2006-01-02"), Tone: "default"},
			},
			Columns: []string{"IP 位址", "狀態", "MAC 位址", "廠商", "網段", "裝置類型", "備註", "上次確認日期", "到期日"},
			Rows:    displayRows,
			PrimaryCTA: &mailer.CTAButton{
				Label: "登入後重新確認，延長一年",
				URL:   "http://localhost:8080/workbench?filter=my_pending_confirmation",
			},
			SecondaryLink: &mailer.CTAButton{
				Label: "若已不使用，請登入後釋放",
				URL:   "http://localhost:8080/workbench",
			},
			FooterLines: []string{
				"您之所以收到此信，是因為您的信箱被填寫為上述位址之擁有者聯絡信箱。",
				"逾期未確認之 IP 紀錄不會被自動刪除，但會於系統標示為已到期，系統管理員可能會與您聯絡。",
			},
		}

		htmlBody, textBody, err := mailer.RenderEmail(tplData)
		if err != nil {
			return sentTotal, fmt.Errorf("產生到期催認郵件失敗: %w", err)
		}

		mailMsg := interfaces.MailMessage{
			To:       []string{mailAddr},
			Cc:       cc,
			Subject:  subject,
			TextBody: textBody,
			HTMLBody: htmlBody,
		}

		var sendErr error
		if sm, ok := s.mailer.(*mailer.SMTPMailer); ok {
			_, sendErr = sm.SendWithTracking(ctx, "expiry_reminder", items[0].DedupKey, mailMsg)
		} else {
			sendErr = s.mailer.Send(ctx, mailMsg)
			s.recordDirectLog(ctx, "expiry_reminder", items[0].DedupKey, []string{mailAddr}, cc, subject, sendErr)
		}

		if sendErr != nil {
			continue // 寄送失敗則不標記為已通知，保留供下次重試
		}

		// 寄送成功：寫入去重狀態
		tx, err := s.db.BeginTx(ctx, nil)
		if err == nil {
			stmt, err := tx.PrepareContext(
				ctx,
				`INSERT INTO notification_states (dedup_key, notif_type, is_notified, notified_at, updated_at)
				 VALUES (?, 'expiry_reminder', TRUE, ?, ?)
				 ON DUPLICATE KEY UPDATE is_notified = TRUE, notified_at = VALUES(notified_at), updated_at = VALUES(updated_at)`,
			)
			if err == nil {
				for _, it := range items {
					_, _ = stmt.ExecContext(ctx, it.DedupKey, now, now)
				}
				_ = stmt.Close()
			}
			_ = tx.Commit()
		}

		sentTotal += len(items)
	}

	return sentTotal, nil
}

// 輔助記錄通知日誌（用於注入 FakeMailer 測試環境下）
func (s *Service) recordDirectLog(ctx context.Context, notifType, dedupKey string, to, cc []string, subject string, err error) {
	if s.db == nil {
		return
	}
	now := s.clock.Now()
	status := "sent"
	errStr := ""
	var sentAt *time.Time
	if err != nil {
		status = "failed"
		errStr = err.Error()
	} else {
		sentAt = &now
	}
	rcpt := strings.Join(to, ", ")
	if len(cc) > 0 {
		rcpt += " (Cc: " + strings.Join(cc, ", ") + ")"
	}
	_, _ = s.db.ExecContext(
		ctx,
		`INSERT INTO notification_logs (notif_type, dedup_key, recipient, subject, status, attempt_count, last_error, sent_at, created_at)
		 VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		notifType, dedupKey, rcpt, subject, status, errStr, sentAt, now,
	)
}
