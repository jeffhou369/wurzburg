package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/config"
	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/iprecords"
	"github.com/jeffhou369/wurzburg/backend/internal/mailer"
	"github.com/jeffhou369/wurzburg/backend/internal/notifications"
	"github.com/jeffhou369/wurzburg/backend/internal/purge"
	"github.com/jeffhou369/wurzburg/backend/internal/server"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
	"github.com/jeffhou369/wurzburg/backend/migrations"
	"github.com/testcontainers/testcontainers-go"
	mariadbmod "github.com/testcontainers/testcontainers-go/modules/mariadb"
)

type fakeMailer struct {
	mu       sync.Mutex
	messages []interfaces.MailMessage
	failNext bool
	failErr  error
}

func (f *fakeMailer) Send(ctx context.Context, msg interfaces.MailMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		if f.failErr != nil {
			return f.failErr
		}
		return errors.New("SMTP connection refused")
	}
	f.messages = append(f.messages, msg)
	return nil
}

func (f *fakeMailer) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = nil
	f.failNext = false
}

func (f *fakeMailer) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.messages)
}

func (f *fakeMailer) PopAll() []interfaces.MailMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]interfaces.MailMessage, len(f.messages))
	copy(out, f.messages)
	f.messages = nil
	return out
}

func TestMailerAndNotificationsE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("略過需啟動 Docker 容器的整合測試")
	}

	ctx := context.Background()
	mariadbContainer, err := mariadbmod.Run(ctx,
		"mariadb:11.4",
		mariadbmod.WithDatabase("wurzburg"),
		mariadbmod.WithUsername("wurzburg"),
		mariadbmod.WithPassword("wurzburg_test_pw"),
	)
	if err != nil {
		t.Fatalf("啟動 MariaDB 容器失敗: %v", err)
	}
	t.Cleanup(func() {
		_ = testcontainers.TerminateContainer(mariadbContainer)
	})

	dsn, _ := mariadbContainer.ConnectionString(ctx, "parseTime=true&loc=UTC")
	db, err := database.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("連線資料庫失敗: %v", err)
	}
	defer db.Close()

	if err := database.RunMigrations(db, migrations.FS, "."); err != nil {
		t.Fatalf("遷移失敗: %v", err)
	}

	// 起始時間設於 2026-11-16（週一）09:00 Asia/Taipei（即 UTC 01:00）
	loc, _ := time.LoadLocation("Asia/Taipei")
	startTime := time.Date(2026, 11, 16, 9, 0, 0, 0, loc).UTC()
	clk := clock.NewTestClock(startTime)

	srv := server.New(&config.Config{
		Port:       8080,
		CampusName: "Würzburg",
		AppEnv:     "test",
		SecretKey:  "0123456789abcdef0123456789abcdef",
	}, db, clk)

	fm := &fakeMailer{}
	smtpM := mailer.NewSMTPMailer(db, clk, srv.Settings)
	mailHandler := mailer.NewHandler(db, clk, srv.Settings, srv.Audit, smtpM)
	mailHandler.SetTestMailer(fm)
	ipHandler := iprecords.NewHandler(db, clk, srv.Settings, srv.Audit)
	purgeSvc := purge.NewService(db, clk, srv.Settings, srv.Audit)

	srv.Mount(mailHandler, ipHandler, purgeSvc)
	router := srv.Router()

	notifSvc := notifications.NewService(db, clk, srv.Settings, srv.Audit, fm)
	tracker := notifications.NewSchedulerTracker(notifSvc)

	seeder := testutil.NewSeeder(t, db, clk)
	adminID := seeder.SeedAdmin("admin")
	user1ID := seeder.SeedUser("chen", "user", "ad", "chen@example.com", "陳工程師")
	user2ID := seeder.SeedUser("lin", "user", "ad", "lin@example.com", "林設計師")
	subID := seeder.SeedSubnet("10.20.0.0/16", "managed", nil)
	swID, vrfID := seeder.SeedSwitch("core-sw", "10.0.0.1", "ios-xe", "v3")

	// =========================================================
	// 1. Ticket 19: 郵件設定、Secret 不回顯、權限控制與測試信
	// =========================================================
	t.Run("Ticket19_MailSettingsAndTestMail", func(t *testing.T) {
		// 一般使用者不得存取郵件設定或寄測試信
		reqUser := testutil.NewAuthRequest(http.MethodGet, "/api/v1/settings/mail", nil, user1ID, "user", "chen")
		wUser := httptest.NewRecorder()
		router.ServeHTTP(wUser, reqUser)
		if wUser.Code != http.StatusForbidden {
			t.Fatalf("一般使用者讀取郵件設定應回 403，實際為 %d", wUser.Code)
		}

		// 管理員更新 SMTP 設定（含密碼）
		updateBody := map[string]any{
			"smtp_host":           "smtp.campus.local",
			"smtp_port":           587,
			"smtp_tls_mode":       "starttls",
			"smtp_username":       "mailer-bot",
			"smtp_password":       "super-secret-smtp-pass",
			"sender_address":      "ipmgmt@campus.local",
			"admin_recipients":    []string{"netadmin@campus.local", "secops@campus.local"},
			"weekly_summary_cron": "0 9 * * 1",
		}
		reqUpd := testutil.NewAuthRequest(http.MethodPut, "/api/v1/settings/mail", updateBody, adminID, "admin", "admin")
		wUpd := httptest.NewRecorder()
		router.ServeHTTP(wUpd, reqUpd)
		if wUpd.Code != http.StatusOK {
			t.Fatalf("管理員更新郵件設定失敗: %d %s", wUpd.Code, wUpd.Body.String())
		}
		if strings.Contains(wUpd.Body.String(), "super-secret-smtp-pass") {
			t.Fatalf("SMTP 密碼外洩於 API 回應中!")
		}
		var mailResp map[string]any
		_ = json.Unmarshal(wUpd.Body.Bytes(), &mailResp)
		if mailResp["has_password"] != true {
			t.Fatalf("應標註 has_password: true, got %v", mailResp["has_password"])
		}

		// 檢查稽核日誌不含密碼明文
		var auditCnt int
		_ = db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'settings.mail_update' AND details_json NOT LIKE '%super-secret-smtp-pass%'`).Scan(&auditCnt)
		if auditCnt != 1 {
			t.Fatalf("稽核日誌未正確記錄或含密碼明文")
		}

		// 寄出測試信
		fm.Reset()
		reqTest := testutil.NewAuthRequest(http.MethodPost, "/api/v1/settings/mail/test", map[string]string{"recipient": "verify@campus.local"}, adminID, "admin", "admin")
		wTest := httptest.NewRecorder()
		router.ServeHTTP(wTest, reqTest)
		if wTest.Code != http.StatusOK {
			t.Fatalf("寄測試信失敗: %d %s", wTest.Code, wTest.Body.String())
		}
		if fm.Count() != 1 {
			t.Fatalf("預期收到 1 封測試信，實際為 %d", fm.Count())
		}
		msgs := fm.PopAll()
		if !strings.Contains(msgs[0].HTMLBody, "<html lang=\"zh-Hant\">") || !strings.Contains(msgs[0].TextBody, "[在線]") {
			t.Errorf("測試信版面或狀態標籤不符規範")
		}
	})

	// =========================================================
	// 2. Ticket 28: 未登記彙整（每小時整點、去重、重試失敗與手動重設）
	// =========================================================
	t.Run("Ticket28_UnregisteredDigest", func(t *testing.T) {
		fm.Reset()
		// 準備：1 筆在線未登記 + 1 筆離線未登記
		ipOn := seeder.SeedIPRecord("10.20.1.10", "aa:bb:cc:11:22:33", subID, &testutil.IPRecordSeedOpts{
			Presence: "online", Registration: "unregistered", VendorName: "Dell Inc.",
		})
		seeder.SeedSighting(ipOn, vrfID, swID, "aa:bb:cc:11:22:33", "Vlan10", 0)

		seeder.SeedIPRecord("10.20.1.11", "aa:bb:cc:11:22:44", subID, &testutil.IPRecordSeedOpts{
			Presence: "offline", Registration: "unregistered",
		})

		// 設成 SMTP 暫時失敗 -> 不可標記為已通知
		fm.failNext = true
		_, err := notifSvc.RunUnregisteredDigest(ctx)
		if err == nil {
			t.Fatalf("預期在 SMTP 失敗時回傳錯誤")
		}

		// 回復正常再觸發整點 -> 應成功寄出，且僅含 10.20.1.10，不含離線的 10.20.1.11
		fm.failNext = false
		sent, err := notifSvc.RunUnregisteredDigest(ctx)
		if err != nil || sent != 1 {
			t.Fatalf("RunUnregisteredDigest 失敗: sent=%d err=%v", sent, err)
		}
		msgs := fm.PopAll()
		if len(msgs) != 1 {
			t.Fatalf("預期寄出 1 封未登記彙整，實際 %d", len(msgs))
		}
		if !strings.Contains(msgs[0].HTMLBody, "10.20.1.10") || strings.Contains(msgs[0].HTMLBody, "10.20.1.11") {
			t.Errorf("彙整內容不符合預期（應含 10.20.1.10 且排除離線位址）")
		}

		// 同一批未登記位址再次觸發 -> 沒有新項目 -> 不寄信
		sent2, err := notifSvc.RunUnregisteredDigest(ctx)
		if err != nil || sent2 != 0 || fm.Count() != 0 {
			t.Fatalf("無新項目時不應再寄信: sent=%d count=%d", sent2, fm.Count())
		}

		// 裝置離線再上線（去重鍵相同）-> 不解除已通知 -> 不重複寄信
		_, _ = db.Exec(`UPDATE ip_records SET presence = 'offline' WHERE id = ?`, ipOn)
		_, _ = notifSvc.RunUnregisteredDigest(ctx)
		_, _ = db.Exec(`UPDATE ip_records SET presence = 'online' WHERE id = ?`, ipOn)
		sent3, _ := notifSvc.RunUnregisteredDigest(ctx)
		if sent3 != 0 || fm.Count() != 0 {
			t.Fatalf("離線再上線不應重複通知: sent=%d", sent3)
		}

		// 未登記 IP 更換 MAC -> 產生新去重鍵 -> 應再次列出為新增
		_, _ = db.Exec(`UPDATE ip_records SET mac_address = 'aa:bb:cc:11:22:99' WHERE id = ?`, ipOn)
		sent4, _ := notifSvc.RunUnregisteredDigest(ctx)
		if sent4 != 1 || fm.Count() != 1 {
			t.Fatalf("更換 MAC 後應再通知一次: sent=%d count=%d", sent4, fm.Count())
		}
		fm.PopAll()

		// 管理員手動重設去重狀態 -> 下次執行再次出現
		dedupKey := url.PathEscape("unreg|10.20.1.10|aa:bb:cc:11:22:99")
		reqReset := testutil.NewAuthRequest(http.MethodPost, "/api/v1/notifications/states/"+dedupKey+"/reset", nil, adminID, "admin", "admin")
		wReset := httptest.NewRecorder()
		router.ServeHTTP(wReset, reqReset)
		if wReset.Code != http.StatusNoContent {
			t.Fatalf("重設通知狀態失敗: %d %s", wReset.Code, wReset.Body.String())
		}
		sent5, _ := notifSvc.RunUnregisteredDigest(ctx)
		if sent5 != 1 || fm.Count() != 1 {
			t.Fatalf("手動重設去重後應再次通知: sent=%d", sent5)
		}
		fm.PopAll()

		// 完成登記會解除未登記通知狀態，之後釋放仍在線會再成為新增項目
		reqReg := testutil.NewAuthRequest(http.MethodPost, "/api/v1/ip-records/actions/register", map[string]any{
			"ip_record_ids": []int64{ipOn},
			"owner_mail":    "chen@example.com",
		}, user1ID, "user", "chen")
		wReg := httptest.NewRecorder()
		router.ServeHTTP(wReg, reqReg)
		if wReg.Code != http.StatusOK {
			t.Fatalf("登記失敗: %d", wReg.Code)
		}
		reqRel := testutil.NewAuthRequest(http.MethodPost, "/api/v1/ip-records/actions/release", map[string]any{
			"ip_record_ids": []int64{ipOn},
		}, user1ID, "user", "chen")
		wRel := httptest.NewRecorder()
		router.ServeHTTP(wRel, reqRel)
		if wRel.Code != http.StatusOK {
			t.Fatalf("釋放失敗: %d", wRel.Code)
		}
		sent6, _ := notifSvc.RunUnregisteredDigest(ctx)
		if sent6 != 1 || fm.Count() != 1 {
			t.Fatalf("登記後釋放仍在線應再通知一次: sent=%d", sent6)
		}
		fm.PopAll()

		// 超過 50 筆截斷驗證（補 55 筆未通知的 IP）
		for i := 1; i <= 55; i++ {
			seeder.SeedIPRecord(fmt.Sprintf("10.20.5.%d", i), fmt.Sprintf("00:50:56:00:00:%02x", i), subID, &testutil.IPRecordSeedOpts{
				Presence: "online", Registration: "unregistered",
			})
		}
		sentMany, _ := notifSvc.RunUnregisteredDigest(ctx)
		if sentMany != 55 {
			t.Fatalf("預期新增 55 筆，實際 %d", sentMany)
		}
		manyMsgs := fm.PopAll()
		if !strings.Contains(manyMsgs[0].HTMLBody, "55 筆") || !strings.Contains(manyMsgs[0].HTMLBody, "僅列出前 50 筆") {
			t.Errorf("超過 50 筆應顯示實際總數與截斷註記")
		}
	})

	// =========================================================
	// 3. Ticket 29: 未登記總表（每週寄送、不改狀態、同週不重複、調整排程）
	// =========================================================
	t.Run("Ticket29_WeeklySummary", func(t *testing.T) {
		fm.Reset()
		// startTime 是 2026-11-16 週一 09:00 Asia/Taipei
		// 目前資料庫內 10.20.1.10 與 10.20.5.1..55 皆處於已通知且在線未登記狀態
		// 先透過 tracker.Tick 在週一 09:00 觸發
		tracker.Tick(ctx)
		msgs := fm.PopAll()
		// Notice: Tick at 09:00:00 also has Minute() == 0, so it runs digest (0 new items) and weekly summary (1 email)
		if len(msgs) != 1 {
			t.Fatalf("週一 09:00 應寄出 1 封未登記總表，實際 %d", len(msgs))
		}
		if !strings.Contains(msgs[0].Subject, "仍未登記的在線位址總表") {
			t.Errorf("主旨不符合未登記總表規格: %s", msgs[0].Subject)
		}

		// 同一週推進 5 分鐘再觸發 -> 同週不得再寄出第二封
		clk.Advance(5 * time.Minute)
		tracker.Tick(ctx)
		if fm.Count() != 0 {
			t.Fatalf("同一週不應重複寄送未登記總表，實際收到 %d 封", fm.Count())
		}

		// 清理測試過多資料僅留少數，並將排程改為週三 14:30 (30 14 * * 3) 測試下週生效
		reqCron := testutil.NewAuthRequest(http.MethodPut, "/api/v1/settings/mail", map[string]any{
			"smtp_host":           "smtp.campus.local",
			"smtp_port":           587,
			"smtp_tls_mode":       "starttls",
			"sender_address":      "ipmgmt@campus.local",
			"admin_recipients":    []string{"netadmin@campus.local"},
			"weekly_summary_cron": "30 14 * * 3",
		}, adminID, "admin", "admin")
		wCron := httptest.NewRecorder()
		router.ServeHTTP(wCron, reqCron)
		if wCron.Code != http.StatusOK {
			t.Fatalf("修改週報 Cron 失敗: %d", wCron.Code)
		}

		// 推進到下週一 09:00 -> 因已改為週三 14:30，週一不應寄送
		nextMon := time.Date(2026, 11, 23, 9, 0, 0, 0, loc).UTC()
		clk.Set(nextMon)
		tracker.Tick(ctx)
		if fm.Count() != 0 {
			t.Fatalf("修改為週三後，下週一 09:00 不應寄送總表")
		}

		// 推進到下週三 14:30 -> 依新設定寄出
		nextWed := time.Date(2026, 11, 25, 14, 30, 0, 0, loc).UTC()
		clk.Set(nextWed)
		tracker.Tick(ctx)
		if fm.Count() != 1 {
			t.Fatalf("修改後於週三 14:30 應寄出總表，實際 %d", fm.Count())
		}
		fm.PopAll()
	})

	// =========================================================
	// 4. Ticket 30: 到期催認（-14d, 0d, +7dx4, 合併信箱, 最終副本, 跳脫）
	// =========================================================
	t.Run("Ticket30_ExpiryRemindersLifecycle", func(t *testing.T) {
		// 先重置測試時間到 2026-12-01 08:00 Asia/Taipei
		baseDay := time.Date(2026, 12, 1, 8, 0, 0, 0, loc).UTC()
		clk.Set(baseDay)
		fm.Reset()

		// 到期日設於 2026-12-15（距今 14 天）
		expDate := time.Date(2026, 12, 15, 0, 0, 0, 0, loc).UTC()

		// 擁有者 1 有兩筆同一天到期的 IP（帶 HTML XSS 備註）
		idOwner1A := seeder.SeedIPRecord("10.20.10.1", "11:22:33:44:55:01", subID, &testutil.IPRecordSeedOpts{
			Presence: "online", Registration: "registered", OwnerUserID: &user1ID, OwnerMail: "chen@example.com",
			ExpiresAt: &expDate, Notes: `<b>重要主機</b><script>bad()</script>`,
		})
		_ = seeder.SeedIPRecord("10.20.10.2", "11:22:33:44:55:02", subID, &testutil.IPRecordSeedOpts{
			Presence: "offline", Registration: "registered", OwnerUserID: &user1ID, OwnerMail: "chen@example.com",
			ExpiresAt: &expDate, Notes: "備援設備",
		})
		// 擁有者 2 有一筆同日到期
		_ = seeder.SeedIPRecord("10.20.10.3", "11:22:33:44:55:03", subID, &testutil.IPRecordSeedOpts{
			Presence: "online", Registration: "registered", OwnerUserID: &user2ID, OwnerMail: "lin@example.com",
			ExpiresAt: &expDate,
		})

		// Round 1 (到期前 14 天: 2026-12-01)
		_, err := notifSvc.RunExpiryReminders(ctx)
		if err != nil {
			t.Fatalf("Round 1 催認執行失敗: %v", err)
		}
		msgs := fm.PopAll()
		if len(msgs) != 2 {
			t.Fatalf("預期 2 個擁有者各自收到 1 封信（chen 的兩筆應合併），實際 %d 封", len(msgs))
		}
		// 檢查 chen 的信件：含兩筆 IP、XSS 跳脫、無 CC、雙連結
		var chenMsg interfaces.MailMessage
		for _, m := range msgs {
			if m.To[0] == "chen@example.com" {
				chenMsg = m
			}
		}
		if !strings.Contains(chenMsg.HTMLBody, "10.20.10.1") || !strings.Contains(chenMsg.HTMLBody, "10.20.10.2") {
			t.Errorf("同信箱兩筆 IP 未正確合併於同一封信中")
		}
		if strings.Contains(chenMsg.HTMLBody, "<script>bad()</script>") {
			t.Errorf("備註中 HTML 標籤未跳脫")
		}
		if len(chenMsg.Cc) != 0 {
			t.Errorf("首次催認不應 CC 給系統管理員")
		}
		if !strings.Contains(chenMsg.TextBody, "若已不使用，請登入後釋放") {
			t.Errorf("純文字版本應包含次要釋放連結")
		}

		// Round 1 隔天重跑（距到期 13 天）-> 同一輪次不重複寄送
		clk.Set(baseDay.AddDate(0, 0, 1))
		_, _ = notifSvc.RunExpiryReminders(ctx)
		if fm.Count() != 0 {
			t.Fatalf("同一輪催認不應重複寄送，收到 %d 封", fm.Count())
		}

		// 模擬 chen 重新確認 10.20.10.1（延長一年），以及 lin 釋放 10.20.10.3
		reqConf := testutil.NewAuthRequest(http.MethodPost, "/api/v1/ip-records/actions/confirm", map[string]any{
			"ip_record_ids": []int64{idOwner1A},
		}, user1ID, "user", "chen")
		wConf := httptest.NewRecorder()
		router.ServeHTTP(wConf, reqConf)
		if wConf.Code != http.StatusOK {
			t.Fatalf("重新確認失敗: %d", wConf.Code)
		}

		// 到期當天 (2026-12-15)：Round 2 寄出，10.20.10.1 已延長一年故只催 10.20.10.2 與 lin
		clk.Set(expDate.Add(8 * time.Hour))
		_, _ = notifSvc.RunExpiryReminders(ctx)
		msgsR2 := fm.PopAll()
		if len(msgsR2) != 2 {
			t.Fatalf("到期當天應寄出 2 封（chen 僅 10.20.10.2，lin 10.20.10.3）")
		}

		// 逾期後 +7d, +14d, +21d 復催 1~3 次（均無 CC）
		for _, daysAfter := range []int{7, 14, 21} {
			clk.Set(expDate.AddDate(0, 0, daysAfter).Add(8 * time.Hour))
			_, _ = notifSvc.RunExpiryReminders(ctx)
			rMsgs := fm.PopAll()
			if len(rMsgs) != 2 {
				t.Fatalf("逾期 +%d 天預期寄 2 封，實際 %d", daysAfter, len(rMsgs))
			}
			for _, rm := range rMsgs {
				if len(rm.Cc) != 0 {
					t.Fatalf("逾期 +%d 天並非最後一次復催，不應 CC 管理員", daysAfter)
				}
			}
		}

		// 逾期 +28d (第 4 次即最後一次復催) -> 必須 CC 給系統管理員
		clk.Set(expDate.AddDate(0, 0, 28).Add(8 * time.Hour))
		_, _ = notifSvc.RunExpiryReminders(ctx)
		finalMsgs := fm.PopAll()
		if len(finalMsgs) != 2 {
			t.Fatalf("最後一次復催預期寄 2 封，實際 %d", len(finalMsgs))
		}
		for _, fmItem := range finalMsgs {
			if len(fmItem.Cc) == 0 || fmItem.Cc[0] != "netadmin@campus.local" {
				t.Fatalf("最後一次復催應副本給系統管理員清單，實際 CC=%v", fmItem.Cc)
			}
		}

		// 逾期 +35d (超過 4 次上限) -> 停止寄信，但擁有者與備註仍保留不自動釋放
		clk.Set(expDate.AddDate(0, 0, 35).Add(8 * time.Hour))
		_, _ = notifSvc.RunExpiryReminders(ctx)
		if fm.Count() != 0 {
			t.Fatalf("超過復催上限後應停止寄信，實際收到 %d 封", fm.Count())
		}
		var retainedOwner sqlNullCheck
		_ = db.QueryRow(`SELECT COALESCE(owner_mail,''), notes FROM ip_records WHERE ip_address = '10.20.10.2'`).Scan(&retainedOwner.mail, &retainedOwner.notes)
		if retainedOwner.mail != "chen@example.com" || retainedOwner.notes != "備援設備" {
			t.Fatalf("催完後應保留擁有者與備註: %+v", retainedOwner)
		}
	})

	// =========================================================
	// 5. 通知日誌保留期清除驗證
	// =========================================================
	t.Run("Ticket19_NotificationLogRetentionCleanup", func(t *testing.T) {
		// 寫入一筆兩年前的舊紀錄
		oldTime := clk.Now().AddDate(-2, 0, 0)
		_, err := db.Exec(
			`INSERT INTO notification_logs (notif_type, dedup_key, recipient, subject, status, attempt_count, created_at)
			 VALUES ('test_mail', '', 'old@example.com', 'old subject', 'sent', 1, ?)`,
			oldTime,
		)
		if err != nil {
			t.Fatalf("插入舊通知紀錄失敗: %v", err)
		}
		deleted, err := notifSvc.CleanupOldLogs(ctx)
		if err != nil || deleted < 1 {
			t.Fatalf("清除過期通知紀錄失敗: deleted=%d err=%v", deleted, err)
		}
	})
}

type sqlNullCheck struct {
	mail  string
	notes string
}
