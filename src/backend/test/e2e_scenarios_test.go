package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// 模擬的 IOS-XE 交換器（預設 VRF）目前回報的內容。情境改動它之後呼叫 apply 寫進 SNMP 模擬器。
type e2eSwitchState struct {
	env        *e2eEnv
	dataName   string // SNMP 模擬器的資料檔名稱（v2c 的 community）
	interfaces []simInterface
	arps       map[string]simARP // 以 IP 為鍵
}

func (s *e2eSwitchState) apply() {
	s.env.t.Helper()
	arps := make([]simARP, 0, len(s.arps))
	for _, a := range s.arps {
		arps = append(arps, a)
	}
	sort.Slice(arps, func(i, j int) bool { return arps[i].IP < arps[j].IP })
	s.env.snmp.setData(s.env.t, s.dataName, buildSNMPRec(s.interfaces, arps))
}

func (s *e2eSwitchState) host(ip, mac string, ifIndex int64) {
	s.arps[ip] = simARP{IP: ip, MAC: mac, IfIndex: ifIndex}
}

func (s *e2eSwitchState) remove(ip string) { delete(s.arps, ip) }

// TestE2EScenarios 走完第 33 張工作項目列出的 13 個情境。
//
// 情境共用同一套系統並依時間先後執行（時鐘只能往前），所以順序與工作項目的編號不完全相同：
// 每週總表（情境 8）發生在到期催認（情境 7）之前。任何一個情境失敗就停止，後面的情境建立在前面的狀態上。
func TestE2EScenarios(t *testing.T) {
	if testing.Short() {
		t.Skip("略過需啟動 Docker 容器的端到端測試")
	}

	iosxeInterfaces := []simInterface{
		{Index: 10, Name: "Vlan10", IP: "10.10.10.254", Mask: "255.255.255.0"},
		{Index: 30, Name: "Vlan30", IP: "10.10.30.254", Mask: "255.255.255.0"},
	}
	// 2026-11-16 是星期一，08:00 Asia/Taipei
	start := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	e := startE2E(t, start, map[string][]byte{
		e2eIOSXECommunity: buildSNMPRec(iosxeInterfaces, nil),
		// 第二台交換器（NX-OS、SNMPv3）在情境 12 才加入系統，但模擬器的資料檔名稱要在啟動時就存在：
		// 預設 VRF 用空白的 context，另一筆 VRF 讀取設定用 context PROD；內容是 test/testdata/snmp/nx-os 的資料檔。
		snmpSimV3DefaultContext: walkFileToSNMPRec(t, "nx-os/default-vrf.walk"),
		"PROD":                  walkFileToSNMPRec(t, "nx-os/vrf-prod.walk"),
	})
	iosxe := &e2eSwitchState{env: e, dataName: e2eIOSXECommunity, interfaces: iosxeInterfaces, arps: map[string]simARP{}}

	run := func(name string, fn func(t *testing.T)) {
		t.Helper()
		if !t.Run(name, fn) {
			t.Fatalf("情境「%s」失敗，後續情境建立在它的狀態上，停止執行", name)
		}
	}

	var admin *e2eSession
	var vlan10ID, vlan30ID int64

	run("情境01_全新安裝", func(t *testing.T) {
		// 以環境變數的初始帳密登入：要求先改密碼，改之前其他 API 都被擋下
		rec, cookie := e.loginAttempt(false, e2eAdminUser, e2eAdminInitialPW)
		if rec.Code != http.StatusOK || cookie == nil {
			t.Fatalf("初始帳密登入失敗: %d %s", rec.Code, rec.Body.String())
		}
		var loginResp struct {
			User struct {
				RequiresPasswordChange bool `json:"requires_password_change"`
			} `json:"user"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &loginResp)
		if !loginResp.User.RequiresPasswordChange {
			t.Fatal("第一次登入應要求修改密碼")
		}
		if blocked := e.rawRequest(http.MethodGet, "/api/v1/switches", nil, cookie); blocked.Code != http.StatusForbidden {
			t.Fatalf("尚未修改初始密碼時應回 403，實際 %d", blocked.Code)
		}
		if chg := e.rawRequest(http.MethodPost, "/api/v1/auth/change-password", map[string]string{"new_password": e2eAdminPW}, cookie); chg.Code != http.StatusOK {
			t.Fatalf("修改初始密碼失敗: %d %s", chg.Code, chg.Body.String())
		}
		if rec, _ := e.loginAttempt(false, e2eAdminUser, e2eAdminInitialPW); rec.Code != http.StatusUnauthorized {
			t.Errorf("改密碼後初始密碼應失效，實際 %d", rec.Code)
		}
		admin = e.login(false, e2eAdminUser, e2eAdminPW)

		wizard := admin.must(http.MethodGet, "/api/v1/settings/wizard", nil, http.StatusOK)
		if wizard["completed"] != false {
			t.Errorf("全新安裝時設定精靈應尚未完成：%v", wizard)
		}

		// 步驟 1：園區資訊
		admin.must(http.MethodPut, "/api/v1/settings/campus", map[string]any{
			"campus_name": "Würzburg 研發園區", "timezone": "Asia/Taipei", "public_base_url": e2ePublicBaseURL,
		}, http.StatusOK)

		// 步驟 2：驗證來源（AD），並以實際的組織帳號測試
		admin.must(http.MethodPut, "/api/v1/settings/auth-source", map[string]any{
			"active_source": "ad", "ad_url": e.ldap.url, "ad_use_tls": false,
			"ad_bind_dn": ldapSimBindDN, "ad_bind_password": ldapSimBindPassword,
			"ad_base_dn": ldapSimBaseDN, "ad_allowed_group": ldapSimAllowedGroup,
			"ad_user_attr": "sAMAccountName", "ad_mail_attr": "mail", "ad_name_attr": "displayName",
		}, http.StatusOK)
		authTest := admin.must(http.MethodPost, "/api/v1/settings/auth-source/test",
			map[string]string{"username": "user01", "password": e2eUser01Password}, http.StatusOK)
		if authTest["success"] != true || authTest["mail"] != e2eUser01Mail {
			t.Fatalf("驗證來源測試應成功並帶回信箱：%v", authTest)
		}

		// 步驟 3：郵件通知，並寄一封測試信
		admin.must(http.MethodPut, "/api/v1/settings/mail", map[string]any{
			"smtp_host": e.mail.host, "smtp_port": e.mail.smtpPort, "smtp_tls_mode": "none",
			"smtp_username": mailSimUser, "smtp_password": mailSimPassword,
			"sender_address": e2eSenderMailbox, "admin_recipients": []string{e2eAdminMailbox},
			"weekly_summary_cron": "0 9 * * 1",
		}, http.StatusOK)
		mailTest := admin.must(http.MethodPost, "/api/v1/settings/mail/test", map[string]string{"recipient": e2eAdminMailbox}, http.StatusOK)
		if mailTest["success"] != true {
			t.Fatalf("測試信應寄送成功：%v", mailTest)
		}
		testMails := e.mail.takeAll(t)
		if len(testMails) != 1 || testMails[0].From != e2eSenderMailbox || len(testMails[0].To) != 1 || testMails[0].To[0] != e2eAdminMailbox {
			t.Fatalf("郵件伺服器應收到 1 封測試信，寄件人與收件人正確：%+v", testMails)
		}
		assertMailFormat(t, testMails[0])

		// 步驟 4：第一台交換器，並以真正的 SNMP 讀取測試
		sw := admin.must(http.MethodPost, "/api/v1/switches", map[string]any{
			"name": "core-iosxe-01", "mgmt_ip": e.snmp.host, "snmp_port": e.snmp.port,
			"platform": "ios-xe", "snmp_version": "v2c", "snmp_community": e2eIOSXECommunity, "timeout_ms": 2000,
		}, http.StatusCreated)
		swID := int64(sw["id"].(float64))
		vrfs := sw["vrf_configs"].([]any)
		if len(vrfs) != 1 {
			t.Fatalf("新交換器應自動帶一筆預設 VRF 讀取設定：%v", vrfs)
		}
		vrfID := int64(vrfs[0].(map[string]any)["id"].(float64))
		testRead := admin.must(http.MethodPost, fmt.Sprintf("/api/v1/switches/%d/vrfs/%d/test-read", swID, vrfID), nil, http.StatusOK)
		if testRead["success"] != true {
			t.Fatalf("測試讀取應成功：%v", testRead)
		}

		// 第一輪輪詢從介面位址表發現網段，等待檢視
		e.poll()
		pending := admin.must(http.MethodGet, "/api/v1/subnets?state=pending", nil, http.StatusOK)
		for _, raw := range pending["items"].([]any) {
			item := raw.(map[string]any)
			switch item["cidr"] {
			case "10.10.10.0/24":
				vlan10ID = int64(item["id"].(float64))
			case "10.10.30.0/24":
				vlan30ID = int64(item["id"].(float64))
			}
		}
		if vlan10ID == 0 || vlan30ID == 0 {
			t.Fatalf("應從介面位址表發現兩個待檢視的網段：%v", pending["items"])
		}

		// 步驟 5：把一個網段設為納管，完成精靈
		admin.must(http.MethodPut, fmt.Sprintf("/api/v1/subnets/%d/state", vlan10ID), map[string]any{"state": "managed"}, http.StatusOK)
		done := admin.must(http.MethodPut, "/api/v1/settings/wizard", map[string]any{"current_step": 5, "completed": true}, http.StatusOK)
		if done["completed"] != true {
			t.Errorf("設定精靈應標為完成：%v", done)
		}
	})

	run("情境02_採集到表格", func(t *testing.T) {
		iosxe.host("10.10.10.50", "00:0c:29:11:22:50", 10) // VMware
		iosxe.host("10.10.10.51", "b8:27:eb:aa:bb:51", 10) // Raspberry Pi
		iosxe.host("10.10.10.52", "00:50:56:11:22:52", 10) // VMware
		iosxe.host("10.10.10.53", "00:0c:29:11:22:53", 10)
		iosxe.host("10.10.10.54", "b8:27:eb:aa:bb:54", 10)
		iosxe.arps["10.10.10.254"] = simARP{IP: "10.10.10.254", MAC: "00:00:0c:07:ac:0a", IfIndex: 10, Type: 4} // 閘道位址自己的 ARP 列
		iosxe.host("10.10.30.10", "00:0c:29:33:00:10", 30)                                                      // 所在網段尚未納管
		iosxe.apply()
		e.poll()

		records := admin.ipRecords(nil)
		got := ipAddresses(records)
		want := []string{"10.10.10.50", "10.10.10.51", "10.10.10.52", "10.10.10.53", "10.10.10.54"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("IP 管理表應只有納管網段內的主機，不含閘道位址與未納管網段：%v", got)
		}
		for _, r := range records {
			if r.Presence != "online" || r.Registration != "unregistered" || r.StateLabel != "在線 · 未登記" {
				t.Errorf("%s 應為在線 · 未登記：%+v", r.IPAddress, r)
			}
		}
		if v := admin.mustIP("10.10.10.50").VendorName; !strings.Contains(v, "VMware") {
			t.Errorf("10.10.10.50 應顯示廠商 VMware，實際 %q", v)
		}
		if v := admin.mustIP("10.10.10.51").VendorName; !strings.Contains(v, "Raspberry") {
			t.Errorf("10.10.10.51 應顯示廠商 Raspberry Pi，實際 %q", v)
		}

		stats := admin.must(http.MethodGet, "/api/v1/dashboard/stats", nil, http.StatusOK)
		if stats["managed_total"] != float64(5) || stats["online_count"] != float64(5) || stats["unregistered_online_count"] != float64(5) {
			t.Errorf("儀表板的數字應與表格一致：%v", stats)
		}
	})

	run("情境03_未登記彙整", func(t *testing.T) {
		// 09:00 郵件伺服器暫時拒收：信沒有寄出，位址不可被標為已通知
		e.mail.rejectRecipients(t, 451)
		e.advanceTo(e.at(2026, 11, 16, 9, 0, 30))
		if mails := e.mail.takeAll(t); len(mails) != 0 {
			t.Fatalf("郵件伺服器拒收時不應有信送達：%d 封", len(mails))
		}
		failed := admin.must(http.MethodGet, "/api/v1/notifications/logs?status=failed", nil, http.StatusOK)
		if len(failed["items"].([]any)) == 0 {
			t.Error("寄送失敗應留下通知紀錄")
		}

		// 10:00 恢復收信：上一小時沒寄成的位址這次要全部列出
		e.mail.acceptAll(t)
		e.advanceTo(e.at(2026, 11, 16, 10, 0, 30))
		mails := e.mail.takeAll(t)
		if len(mails) != 1 {
			t.Fatalf("整點應收到 1 封未登記彙整，實際 %d 封", len(mails))
		}
		digest := mails[0]
		assertMailFormat(t, digest)
		if len(digest.To) != 1 || digest.To[0] != e2eAdminMailbox {
			t.Errorf("未登記彙整應寄給系統管理員：%v", digest.To)
		}
		hosts := []string{"10.10.10.50", "10.10.10.51", "10.10.10.52", "10.10.10.53", "10.10.10.54"}
		if !containsAll(digest.Text, hosts...) || !containsAll(digest.HTML, hosts...) {
			t.Errorf("純文字與 HTML 本文都要列出全部未登記位址")
		}
		if strings.Contains(digest.Text, "10.10.10.254") || strings.Contains(digest.Text, "10.10.30.10") {
			t.Errorf("彙整不應包含閘道位址或未納管網段的位址")
		}
		if !strings.Contains(digest.Text, "未登記") || !strings.Contains(digest.HTML, "未登記") {
			t.Errorf("兩種本文都要以文字表達狀態")
		}
		wantLink := e2ePublicBaseURL + "/?presence=online&registration=unregistered"
		if !strings.Contains(digest.Text, wantLink) {
			t.Errorf("彙整信應附上通往未登記在線清單的連結 %s", wantLink)
		}

		// 11:00 沒有新的位址：不再寄信
		e.advanceTo(e.at(2026, 11, 16, 11, 0, 30))
		if mails := e.mail.takeAll(t); len(mails) != 0 {
			t.Fatalf("沒有新的未登記位址時不應再寄信：%d 封", len(mails))
		}
	})

	var user01, user02 *e2eSession

	run("情境04_登記", func(t *testing.T) {
		if rec, _ := e.loginAttempt(true, "user01", "WrongUserPass#01"); rec.Code != http.StatusUnauthorized {
			t.Errorf("組織帳號密碼錯誤應回 401，實際 %d", rec.Code)
		}
		if rec, _ := e.loginAttempt(true, "outsider", e2eOutsiderPass); rec.Code != http.StatusForbidden {
			t.Errorf("不在允許群組的組織帳號應回 403，實際 %d %s", rec.Code, rec.Body.String())
		}
		user01 = e.login(true, "user01", e2eUser01Password)
		user02 = e.login(true, "user02", e2eUser02Password)
		me := user01.must(http.MethodGet, "/api/v1/auth/me", nil, http.StatusOK)
		if me["display_name"] != "王工程師" || me["role"] != "user" {
			t.Errorf("組織帳號的顯示名稱與角色應來自驗證來源：%v", me)
		}

		// 單筆登記
		ip50 := user01.mustIP("10.10.10.50")
		single := user01.must(http.MethodPost, "/api/v1/ip-records/actions/register", map[string]any{
			"ip_record_ids": []int64{ip50.ID}, "owner_mail": e2eUser01Mail, "device_type": "虛擬伺服器", "notes": "測試用資料庫主機",
		}, http.StatusOK)
		if single["succeeded_count"] != float64(1) {
			t.Fatalf("單筆登記應成功：%v", single)
		}
		// 批次登記
		ip52, ip53 := user01.mustIP("10.10.10.52"), user01.mustIP("10.10.10.53")
		batch := user01.must(http.MethodPost, "/api/v1/ip-records/actions/register", map[string]any{
			"ip_record_ids": []int64{ip52.ID, ip53.ID}, "owner_mail": e2eUser01Mail, "device_type": "工作站",
		}, http.StatusOK)
		if batch["succeeded_count"] != float64(2) {
			t.Fatalf("批次登記應成功 2 筆：%v", batch)
		}
		for _, addr := range []string{"10.10.10.50", "10.10.10.52", "10.10.10.53"} {
			if r := admin.mustIP(addr); r.Registration != "registered" || r.OwnerUsername != "user01" || r.StateLabel != "在線 · 已登記" {
				t.Errorf("%s 應為 user01 的已登記位址：%+v", addr, r)
			}
		}

		// 授權在 API 逐筆判斷：別人不能編輯或釋放 user01 的登記；已登記的位址不能被別人再登記
		if rec := user02.do(http.MethodPut, fmt.Sprintf("/api/v1/ip-records/%d", ip50.ID), map[string]any{"owner_mail": e2eUser02Mail, "notes": "竄改"}); rec.Code != http.StatusForbidden {
			t.Errorf("非擁有者編輯登記應回 403，實際 %d", rec.Code)
		}
		release := user02.must(http.MethodPost, "/api/v1/ip-records/actions/release", map[string]any{"ip_record_ids": []int64{ip50.ID}}, http.StatusOK)
		if release["succeeded_count"] != float64(0) {
			t.Errorf("非擁有者不可釋放別人的登記：%v", release)
		}
		if r := admin.mustIP("10.10.10.50"); r.OwnerUsername != "user01" || r.Notes != "測試用資料庫主機" {
			t.Errorf("user01 的登記不應被別人改動：%+v", r)
		}

		// 12:00 已登記的位址不再出現在未登記彙整；其餘位址先前已通知過
		e.advanceTo(e.at(2026, 11, 16, 12, 0, 30))
		if mails := e.mail.takeAll(t); len(mails) != 0 {
			t.Fatalf("登記後不應再收到未登記彙整：%d 封", len(mails))
		}
		stats := admin.must(http.MethodGet, "/api/v1/dashboard/stats", nil, http.StatusOK)
		if stats["unregistered_online_count"] != float64(2) {
			t.Errorf("儀表板的未登記在線數應為 2：%v", stats)
		}
	})

	run("情境05_離線與恢復", func(t *testing.T) {
		const addr = "10.10.10.51"
		original := iosxe.arps[addr]

		// 交換器不再回報：第 1 輪仍在寬限內
		iosxe.remove(addr)
		iosxe.apply()
		e.poll()
		if r := admin.mustIP(addr); r.Presence != "online" {
			t.Fatalf("第 1 輪沒看到時仍應在線，實際 %s", r.Presence)
		}

		// 讀取失敗的一輪：不能算成沒看到
		e.snmp.removeData(t, e2eIOSXECommunity)
		e.poll()
		if r := admin.mustIP(addr); r.Presence != "online" {
			t.Fatalf("讀取失敗的輪次不可讓位址離線，實際 %s", r.Presence)
		}
		// 讀取失敗要讓系統管理員看得到：儀表板的讀取狀態，以及該筆 VRF 讀取設定的最近結果
		stats := admin.must(http.MethodGet, "/api/v1/dashboard/stats", nil, http.StatusOK)
		if stats["failed_vrf_count"] != float64(1) || stats["last_sync_success"] != false {
			t.Errorf("讀取失敗的輪次應反映在儀表板：%v", stats)
		}
		if st := firstVRFStatus(t, admin); st["last_status"] == "success" || st["last_error_class"] == "" {
			t.Errorf("VRF 讀取設定應記錄讀取失敗與原因分類：%v", st)
		}
		if r := admin.mustIP("10.10.10.50"); r.Presence != "online" {
			t.Errorf("讀取失敗的輪次不可讓任何位址離線：%+v", r)
		}

		// 恢復讀取，連續第 2 輪沒看到：離線
		iosxe.apply()
		e.poll()
		if stats := admin.must(http.MethodGet, "/api/v1/dashboard/stats", nil, http.StatusOK); stats["failed_vrf_count"] != float64(0) || stats["last_sync_success"] != true {
			t.Errorf("恢復讀取後儀表板應回到正常：%v", stats)
		}
		if r := admin.mustIP(addr); r.Presence != "offline" || r.StateLabel != "離線 · 未登記" {
			t.Fatalf("連續 2 輪沒看到應為離線 · 未登記：%+v", r)
		}
		if got := ipAddresses(admin.ipRecords(url.Values{"presence": {"offline"}})); strings.Join(got, ",") != addr {
			t.Errorf("離線篩選應只有 %s：%v", addr, got)
		}

		// 重新回報：恢復在線
		iosxe.arps[addr] = original
		iosxe.apply()
		e.poll()
		if r := admin.mustIP(addr); r.Presence != "online" {
			t.Fatalf("重新回報後應恢復在線，實際 %s", r.Presence)
		}
	})

	run("情境06_衝突", func(t *testing.T) {
		iosxe.host("10.10.10.50", "00:0c:29:99:99:50", 10) // 已登記的位址換了 MAC
		iosxe.host("10.10.10.51", "b8:27:eb:77:77:51", 10) // 未登記的位址換了 MAC
		iosxe.apply()
		e.poll()

		open := admin.must(http.MethodGet, "/api/v1/conflicts?status=open", nil, http.StatusOK)
		items := open["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("只有已登記的位址換 MAC 才開衝突，實際 %d 筆：%v", len(items), items)
		}
		conflict := items[0].(map[string]any)
		if conflict["ip_address"] != "10.10.10.50" || conflict["adopted_mac"] != "00:0c:29:11:22:50" {
			t.Fatalf("衝突內容不符：%v", conflict)
		}
		conflictID := int64(conflict["id"].(float64))
		if r := admin.mustIP("10.10.10.50"); !r.HasConflict || r.MACAddress != "00:0c:29:11:22:50" || !strings.Contains(r.StateLabel, "衝突") {
			t.Errorf("衝突未裁決前採信的 MAC 不變，狀態文字要標示衝突：%+v", r)
		}
		if r := admin.mustIP("10.10.10.51"); r.HasConflict || r.MACAddress != "b8:27:eb:77:77:51" {
			t.Errorf("未登記的位址換 MAC 應直接更新、不開衝突：%+v", r)
		}

		// 只有系統管理員可以裁決
		resolveBody := map[string]any{"resolution": "adopt_new_keep_reg", "adopted_mac": "00:0c:29:99:99:50", "note": "確認更換網卡"}
		resolvePath := fmt.Sprintf("/api/v1/conflicts/%d/resolve", conflictID)
		if rec := user01.do(http.MethodPost, resolvePath, resolveBody); rec.Code != http.StatusForbidden {
			t.Errorf("一般使用者裁決衝突應回 403，實際 %d", rec.Code)
		}
		admin.must(http.MethodPost, resolvePath, resolveBody, http.StatusOK)
		if r := admin.mustIP("10.10.10.50"); r.HasConflict || r.MACAddress != "00:0c:29:99:99:50" || r.Registration != "registered" || r.OwnerUsername != "user01" {
			t.Errorf("裁決後應採信新的 MAC 並保留登記：%+v", r)
		}
		if left := admin.must(http.MethodGet, "/api/v1/conflicts?status=open", nil, http.StatusOK); len(left["items"].([]any)) != 0 {
			t.Errorf("裁決後不應還有未處理的衝突：%v", left["items"])
		}

		// 13:00 未登記的位址換了 MAC，要再出現在未登記彙整；已通知過且沒變的位址不重複
		e.advanceTo(e.at(2026, 11, 16, 13, 0, 30))
		mails := e.mail.takeAll(t)
		if len(mails) != 1 {
			t.Fatalf("未登記的位址換 MAC 後應再收到 1 封彙整，實際 %d 封", len(mails))
		}
		if !strings.Contains(mails[0].Text, "10.10.10.51") || strings.Contains(mails[0].Text, "10.10.10.54") || strings.Contains(mails[0].Text, "10.10.10.50") {
			t.Errorf("彙整應只列出換了 MAC 的未登記位址 10.10.10.51：%s", mails[0].Text)
		}
	})

	run("情境08_未登記總表", func(t *testing.T) {
		// 下週一 09:00：每週的寄送時間
		e.advanceTo(e.at(2026, 11, 23, 9, 0, 30))
		mails := e.mail.takeAll(t)
		var summaries []simMail
		for _, m := range mails {
			if strings.Contains(m.Subject, "總表") {
				summaries = append(summaries, m)
			}
		}
		if len(summaries) != 1 {
			t.Fatalf("每週寄送時間應收到 1 封未登記總表，實際收到的信：%v", mailSubjects(mails))
		}
		summary := summaries[0]
		assertMailFormat(t, summary)
		if len(summary.To) != 1 || summary.To[0] != e2eAdminMailbox {
			t.Errorf("未登記總表應寄給系統管理員：%v", summary.To)
		}
		for _, body := range []string{summary.Text, summary.HTML} {
			if !containsAll(body, "10.10.10.51", "10.10.10.54") {
				t.Errorf("總表應列出仍未登記的在線位址")
			}
			if strings.Contains(body, "10.10.10.50") || strings.Contains(body, "10.10.10.52") {
				t.Errorf("總表不應列出已登記的位址")
			}
		}

		// 同一個寄送時段內再次檢查排程，不重複寄
		e.advanceTo(e.at(2026, 11, 23, 9, 10, 0))
		if again := e.mail.takeAll(t); len(again) != 0 {
			t.Fatalf("同一週的總表不應重複寄送：%v", mailSubjects(again))
		}
	})

	run("情境11_匯出", func(t *testing.T) {
		// user02 登記一筆，備註與主機名稱以試算表公式的開頭字元起始
		ip54 := user02.mustIP("10.10.10.54")
		user02.must(http.MethodPost, "/api/v1/ip-records/actions/register", map[string]any{
			"ip_record_ids": []int64{ip54.ID}, "owner_mail": e2eUser02Mail,
			"hostname": "+SUM(1,2)", "notes": "=CMD|C!calc",
		}, http.StatusOK)

		// 匯出與畫面使用相同的篩選條件，結果要一致
		for _, tc := range []struct {
			name    string
			session *e2eSession
			query   url.Values
		}{
			{"全部", admin, nil},
			{"未登記", admin, url.Values{"registration": {"unregistered"}}},
			{"已登記且在線", admin, url.Values{"registration": {"registered"}, "presence": {"online"}}},
			{"搜尋", admin, url.Values{"search": {"10.10.10.5"}}},
			{"我的登記", user02, url.Values{"owner_me": {"true"}}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				table := tc.session.ipRecords(tc.query)
				rows := tc.session.export(tc.query)
				if len(rows) != len(table)+1 {
					t.Fatalf("匯出應有 1 列標題加 %d 列資料，實際 %d 列", len(table), len(rows))
				}
				for i, rec := range table {
					row := strings.Join(rows[i+1], "|")
					if !containsAll(row, rec.IPAddress, rec.MACAddress, rec.StateLabel) {
						t.Errorf("匯出第 %d 列應與畫面的 %s 一致（含狀態文字 %q）：%s", i+1, rec.IPAddress, rec.StateLabel, row)
					}
				}
			})
		}

		// 公式開頭的內容要跳脫，不能被試算表當成公式執行
		row54 := strings.Join(findRow(t, admin.export(nil), "10.10.10.54"), "|")
		if !containsAll(row54, "'=CMD|C!calc", "'+SUM(1,2)") {
			t.Errorf("匯出時公式開頭的字元應加上單引號跳脫：%s", row54)
		}
	})

	run("情境07_到期", func(t *testing.T) {
		const pendingLink = e2ePublicBaseURL + "/?owner_me=true&pending_confirmation=true"
		// user01 在 2026-11-16 登記了三筆，有效 365 天，到期日 2027-11-16。到期前 14 天寄第一封催認。
		e.advanceTo(e.at(2027, 11, 2, 8, 30, 0))
		first := mailsTo(e.mail.takeAll(t), e2eUser01Mail)
		if len(first) != 1 {
			t.Fatalf("到期前 14 天擁有者應收到 1 封催認信，實際 %d 封", len(first))
		}
		assertMailFormat(t, first[0])
		if len(first[0].Cc) != 0 {
			t.Errorf("第一封催認不應有副本：%v", first[0].Cc)
		}
		for _, body := range []string{first[0].Text, first[0].HTML} {
			if !containsAll(body, "10.10.10.50", "10.10.10.52", "10.10.10.53") {
				t.Errorf("同一位擁有者的多筆到期登記應合併在一封信")
			}
		}
		if !strings.Contains(first[0].Text, pendingLink) {
			t.Errorf("催認信應附上通往待確認清單的連結 %s", pendingLink)
		}

		// 信中的連結對應的清單：登入後依帳號過濾，只有自己的三筆
		pending := user01.ipRecords(url.Values{"owner_me": {"true"}, "pending_confirmation": {"true"}})
		if got := strings.Join(ipAddresses(pending), ","); got != "10.10.10.50,10.10.10.52,10.10.10.53" {
			t.Fatalf("待確認清單應是 user01 即將到期的三筆：%s", got)
		}
		for _, r := range pending {
			if !r.ExpiringSoon || !strings.Contains(r.StateLabel, "即將到期") {
				t.Errorf("%s 應標示即將到期：%+v", r.IPAddress, r)
			}
		}

		// 批次重新確認其中兩筆
		confirm := user01.must(http.MethodPost, "/api/v1/ip-records/actions/confirm", map[string]any{
			"ip_record_ids": []int64{pending[0].ID, pending[1].ID},
		}, http.StatusOK)
		if confirm["succeeded_count"] != float64(2) {
			t.Fatalf("批次重新確認應成功 2 筆：%v", confirm)
		}
		if r := user01.mustIP("10.10.10.50"); r.ExpiringSoon || r.StateLabel != "在線 · 已登記" {
			t.Errorf("重新確認後不應再標示即將到期：%+v", r)
		}

		// 隔天：已重新確認的不再催；沒處理的那筆這一輪已寄過，也不重複
		e.advanceTo(e.at(2027, 11, 3, 8, 30, 0))
		if again := mailsTo(e.mail.takeAll(t), e2eUser01Mail); len(again) != 0 {
			t.Fatalf("重新確認後不應再收到催認信：%v", mailSubjects(again))
		}

		// 沒處理的那筆：到期當天、之後每 7 天各一封，共 4 次逾期催認，最後一次副本給系統管理員
		steps := []struct {
			when  time.Time
			final bool
		}{
			{e.at(2027, 11, 16, 8, 30, 0), false}, // 到期當天
			{e.at(2027, 11, 23, 8, 30, 0), false}, // 逾期 7 天
			{e.at(2027, 11, 30, 8, 30, 0), false},
			{e.at(2027, 12, 7, 8, 30, 0), false},
			{e.at(2027, 12, 14, 8, 30, 0), true}, // 最後一次
		}
		for _, step := range steps {
			e.advanceTo(step.when)
			got := mailsTo(e.mail.takeAll(t), e2eUser01Mail)
			if len(got) != 1 {
				t.Fatalf("%s 擁有者應收到 1 封催認信，實際 %d 封", step.when.Format("2006-01-02"), len(got))
			}
			if !strings.Contains(got[0].Text, "10.10.10.53") || strings.Contains(got[0].Text, "10.10.10.50") {
				t.Errorf("%s 的催認信應只列出沒處理的 10.10.10.53", step.when.Format("2006-01-02"))
			}
			hasAdminCopy := strings.Join(got[0].Cc, ",") == e2eAdminMailbox
			if hasAdminCopy != step.final {
				t.Errorf("%s 的催認信副本不符（只有最後一次要給系統管理員）：%v", step.when.Format("2006-01-02"), got[0].Cc)
			}
		}

		// 催到上限後不再寄信，也不自動釋放：維持已到期、擁有者不變
		e.advanceTo(e.at(2027, 12, 21, 8, 30, 0))
		if more := mailsTo(e.mail.takeAll(t), e2eUser01Mail); len(more) != 0 {
			t.Fatalf("催認次數到上限後不應再寄信：%v", mailSubjects(more))
		}
		if r := admin.mustIP("10.10.10.53"); r.Registration != "expired" || r.OwnerUsername != "user01" || !strings.Contains(r.StateLabel, "已到期") {
			t.Errorf("沒處理的登記應維持已到期並保留擁有者：%+v", r)
		}
		if got := ipAddresses(admin.ipRecords(url.Values{"registration": {"expired"}})); !containsAll(strings.Join(got, ","), "10.10.10.53") {
			t.Errorf("已到期篩選應包含 10.10.10.53：%v", got)
		}
	})

	run("情境09_排除範圍與不納管", func(t *testing.T) {
		// 排除範圍：範圍內有兩筆已登記的 IP 紀錄
		exclusion := map[string]any{"start_ip": "10.10.10.52", "end_ip": "10.10.10.53", "reason": "dhcp"}
		exclusionsPath := fmt.Sprintf("/api/v1/subnets/%d/exclusions", vlan10ID)
		preview := admin.must(http.MethodPost, exclusionsPath+"/preview", exclusion, http.StatusOK)
		if preview["total_ip_records"] != float64(2) || preview["owned_ip_records"] != float64(2) || preview["requires_confirm"] != true {
			t.Fatalf("預覽應指出會刪除 2 筆、其中 2 筆有擁有者：%v", preview)
		}
		if rec := admin.do(http.MethodPost, exclusionsPath, exclusion); rec.Code != http.StatusConflict {
			t.Fatalf("沒有經過預覽確認就建立排除範圍應回 409，實際 %d", rec.Code)
		}
		if admin.ip("10.10.10.52") == nil {
			t.Fatal("未確認前 IP 紀錄不可被刪除")
		}
		confirmed := map[string]any{"start_ip": "10.10.10.52", "end_ip": "10.10.10.53", "reason": "dhcp", "preview_token": preview["preview_token"]}
		admin.must(http.MethodPost, exclusionsPath, confirmed, http.StatusCreated)
		if admin.ip("10.10.10.52") != nil || admin.ip("10.10.10.53") != nil {
			t.Fatal("排除範圍內的 IP 紀錄應在確認後刪除")
		}
		if findAudit(t, admin, "10.10.10.52", "10.10.10.53", "user01", e2eUser01Mail) == "" {
			t.Errorf("稽核紀錄應留下被刪除的位址與擁有者明細")
		}
		// 交換器仍回報這兩個位址：排除範圍內不可重新建立 IP 紀錄
		e.poll()
		if admin.ip("10.10.10.52") != nil || admin.ip("10.10.10.53") != nil {
			t.Error("排除範圍內的位址不應重新建立 IP 紀錄")
		}

		// 不納管：先把另一個網段納管，讓它有一筆已登記的 IP 紀錄
		statePath := fmt.Sprintf("/api/v1/subnets/%d/state", vlan30ID)
		admin.must(http.MethodPut, statePath, map[string]any{"state": "managed"}, http.StatusOK)
		e.poll()
		ip3010 := user02.mustIP("10.10.30.10")
		user02.must(http.MethodPost, "/api/v1/ip-records/actions/register", map[string]any{
			"ip_record_ids": []int64{ip3010.ID}, "owner_mail": e2eUser02Mail,
		}, http.StatusOK)

		unmanage := map[string]any{"state": "unmanaged", "exclude_reason": "other", "exclude_comment": "改由其他系統管理"}
		statePreview := admin.must(http.MethodPost, statePath+"/preview", unmanage, http.StatusOK)
		if statePreview["total_ip_records"] != float64(1) || statePreview["owned_ip_records"] != float64(1) {
			t.Fatalf("預覽應指出會刪除 1 筆、其中 1 筆有擁有者：%v", statePreview)
		}
		if rec := admin.do(http.MethodPut, statePath, unmanage); rec.Code != http.StatusConflict {
			t.Fatalf("沒有經過預覽確認就改為不納管應回 409，實際 %d", rec.Code)
		}
		unmanage["preview_token"] = statePreview["preview_token"]
		admin.must(http.MethodPut, statePath, unmanage, http.StatusOK)
		if admin.ip("10.10.30.10") != nil {
			t.Fatal("網段改為不納管後，IP 紀錄應刪除")
		}
		if findAudit(t, admin, "10.10.30.10", "user02", e2eUser02Mail) == "" {
			t.Errorf("稽核紀錄應留下被刪除的位址與擁有者明細")
		}
		e.poll()
		if admin.ip("10.10.30.10") != nil {
			t.Error("不納管的網段不應重新建立 IP 紀錄")
		}
	})

	run("情境10_自動清除", func(t *testing.T) {
		// 一筆未登記、一筆已登記的位址同時離線
		iosxe.remove("10.10.10.51")
		iosxe.remove("10.10.10.50")
		iosxe.apply()
		e.poll()
		e.poll()
		if a, b := admin.mustIP("10.10.10.51"), admin.mustIP("10.10.10.50"); a.Presence != "offline" || b.Presence != "offline" {
			t.Fatalf("兩筆都應離線：%+v %+v", a, b)
		}

		// 第 89 天：還在保留期內
		e.advanceTo(e.at(2028, 3, 18, 8, 30, 0))
		if admin.ip("10.10.10.51") == nil {
			t.Fatal("未登記的位址離線未滿 90 天不應清除")
		}
		// 超過 90 天：未登記的消失，已登記的離線多久都保留
		e.advanceTo(e.at(2028, 3, 23, 8, 30, 0))
		if admin.ip("10.10.10.51") != nil {
			t.Fatal("未登記且離線超過 90 天的位址應自動清除")
		}
		if r := admin.mustIP("10.10.10.50"); r.Registration != "registered" || r.Presence != "offline" || r.OwnerUsername != "user01" {
			t.Errorf("已登記的位址離線多久都要保留：%+v", r)
		}
		if entry := findAudit(t, admin, "ip_records.auto_purge"); entry == "" {
			t.Error("自動清除應留下稽核紀錄")
		}
	})

	var nxosVRFs map[string]int64 // VRF 名稱 → VRF 讀取設定 ID

	run("情境12_兩個平台與VRF", func(t *testing.T) {
		// 第二台交換器：NX-OS、SNMPv3（驗證加加密），回放 test/testdata/snmp/nx-os 的資料檔。
		sw := admin.must(http.MethodPost, "/api/v1/switches", map[string]any{
			"name": "core-nxos-01", "mgmt_ip": secondLocalIPv4(t), "snmp_port": e.snmp.port,
			"platform": "nx-os", "snmp_version": "v3", "timeout_ms": 2000,
			"v3_username": simV3User, "v3_auth_protocol": "SHA", "v3_auth_pass": simV3AuthPass,
			"v3_priv_protocol": "AES", "v3_priv_pass": simV3PrivPass,
		}, http.StatusCreated)
		nxosID := int64(sw["id"].(float64))
		created := admin.must(http.MethodPost, fmt.Sprintf("/api/v1/switches/%d/vrfs", nxosID),
			map[string]any{"vrf_name": "PROD", "snmp_context": "PROD"}, http.StatusCreated)
		nxosVRFs = map[string]int64{}
		for _, raw := range created["items"].([]any) {
			v := raw.(map[string]any)
			nxosVRFs[v["vrf_name"].(string)] = int64(v["id"].(float64))
		}
		if len(nxosVRFs) != 2 {
			t.Fatalf("NX-OS 交換器應有兩筆 VRF 讀取設定：%v", created["items"])
		}

		// 第一輪：兩筆 VRF 讀取設定各自發現網段；納管後下一輪位址出現
		e.poll()
		for name, status := range vrfStatuses(t, admin, nxosID) {
			if !strings.HasPrefix(status, "success") {
				t.Fatalf("NX-OS 的 VRF %s 第一輪應讀取成功：%s", name, status)
			}
		}
		for _, cidr := range []string{"10.10.20.0/24", "10.200.0.0/24"} {
			id := subnetID(t, admin, cidr)
			admin.must(http.MethodPut, fmt.Sprintf("/api/v1/subnets/%d/state", id), map[string]any{"state": "managed"}, http.StatusOK)
		}
		e.poll()
		all := strings.Join(ipAddresses(admin.ipRecords(nil)), ",")
		for _, addr := range []string{
			"10.10.10.54",                // IOS-XE 預設 VRF
			"10.10.20.200",               // NX-OS 預設 VRF
			"10.200.0.10", "10.200.0.11", // NX-OS VRF PROD
		} {
			if !strings.Contains(all, addr) {
				t.Errorf("三筆 VRF 讀取設定的位址都應出現，缺少 %s：%s", addr, all)
			}
		}
		if strings.Contains(all, "10.10.20.3") || strings.Contains(all, "10.200.0.1,") {
			t.Errorf("交換器自己的介面位址是閘道位址，不應有 IP 紀錄：%s", all)
		}
		// NX-OS 的大編號 ifIndex 要能對回介面名稱
		detail := admin.must(http.MethodGet, fmt.Sprintf("/api/v1/ip-records/%d", admin.mustIP("10.200.0.10").ID), nil, http.StatusOK)
		if names, _ := json.Marshal(detail["if_names"]); !strings.Contains(string(names), "Vlan200") {
			t.Errorf("10.200.0.10 的來源介面應為 Vlan200：%s", names)
		}

		// VRF PROD 讀不到：只有它失敗，其他兩筆照常更新，PROD 的位址也不因此離線
		e.snmp.removeData(t, "PROD")
		iosxe.host("10.10.10.55", "00:0c:29:11:22:55", 10)
		iosxe.apply()
		e.poll()
		stats := admin.must(http.MethodGet, "/api/v1/dashboard/stats", nil, http.StatusOK)
		if stats["failed_vrf_count"] != float64(1) {
			t.Errorf("應只有 1 筆 VRF 讀取設定失敗：%v", stats)
		}
		for name, status := range vrfStatuses(t, admin, nxosID) {
			if failed := !strings.HasPrefix(status, "success"); failed != (name == "PROD") {
				t.Errorf("VRF %s 的讀取狀態不符：%s", name, status)
			}
		}
		if admin.ip("10.10.10.55") == nil {
			t.Error("另一台交換器新回報的位址應照常出現")
		}
		if r := admin.mustIP("10.200.0.10"); r.Presence != "online" {
			t.Errorf("讀取失敗的 VRF，其位址不應變成離線：%+v", r)
		}

		// 恢復後回到正常
		e.snmp.setData(t, "PROD", walkFileToSNMPRec(t, "nx-os/vrf-prod.walk"))
		e.poll()
		if stats := admin.must(http.MethodGet, "/api/v1/dashboard/stats", nil, http.StatusOK); stats["failed_vrf_count"] != float64(0) {
			t.Errorf("恢復後不應有讀取失敗的 VRF：%v", stats)
		}
	})

	run("情境13_故障隔離", func(t *testing.T) {
		// 郵件伺服器與驗證來源同時中斷
		e.mail.stop(t)
		e.ldap.stop(t)

		// 採集照常：新的位址照樣進來
		iosxe.host("10.10.10.56", "00:0c:29:11:22:56", 10)
		iosxe.apply()
		e.poll()
		if r := admin.mustIP("10.10.10.56"); r.Presence != "online" {
			t.Fatalf("外部系統中斷時採集應照常運作：%+v", r)
		}

		// 通知寄不出去：排程不中斷，留下失敗紀錄，位址不被標為已通知
		e.advanceTo(e.at(2028, 3, 23, 10, 0, 30))
		failed := admin.must(http.MethodGet, "/api/v1/notifications/logs?status=failed", nil, http.StatusOK)
		if !strings.Contains(fmt.Sprint(failed["items"]), "未登記") {
			t.Errorf("寄不出去的未登記彙整應留下失敗的通知紀錄")
		}

		// 網頁照常：系統管理員可以重新登入並操作
		fresh := e.login(false, e2eAdminUser, e2eAdminPW)
		fresh.must(http.MethodGet, "/api/v1/dashboard/stats", nil, http.StatusOK)
		fresh.must(http.MethodGet, "/api/v1/switches", nil, http.StatusOK)

		// 組織帳號無法登入，而且要說明是驗證服務的問題，不是帳密錯誤
		rec, _ := e.loginAttempt(true, "user01", e2eUser01Password)
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "auth_unavailable") {
			t.Errorf("驗證來源中斷時組織帳號登入應回 503 auth_unavailable，實際 %d %s", rec.Code, rec.Body.String())
		}
	})
}

// export 以指定的篩選條件下載 Excel 匯出，回傳工作表的每一列。
func (s *e2eSession) export(query url.Values) [][]string {
	s.env.t.Helper()
	rec := s.do(http.MethodGet, "/api/v1/ip-records/export?"+query.Encode(), nil)
	if rec.Code != http.StatusOK {
		s.env.t.Fatalf("匯出失敗: %d %s", rec.Code, rec.Body.String())
	}
	xf, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		s.env.t.Fatalf("匯出檔不是合法的 Excel: %v", err)
	}
	defer func() { _ = xf.Close() }()
	rows, err := xf.GetRows(xf.GetSheetName(0))
	if err != nil {
		s.env.t.Fatalf("讀取匯出檔失敗: %v", err)
	}
	return rows
}

func findRow(t *testing.T, rows [][]string, ip string) []string {
	t.Helper()
	for _, r := range rows {
		for _, cell := range r {
			if cell == ip {
				return r
			}
		}
	}
	t.Fatalf("匯出檔中找不到 %s", ip)
	return nil
}

func mailsTo(mails []simMail, recipient string) []simMail {
	var out []simMail
	for _, m := range mails {
		for _, to := range m.To {
			if to == recipient {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

// findAudit 經由稽核 API 找出最新一筆內容同時含有所有 needles 的稽核紀錄，回傳整筆紀錄的 JSON；找不到時回傳空字串。
func findAudit(t *testing.T, s *e2eSession, needles ...string) string {
	t.Helper()
	logs := s.must(http.MethodGet, "/api/v1/audit/logs?page_size=200", nil, http.StatusOK)
	for _, raw := range logs["items"].([]any) {
		entry, _ := json.Marshal(raw)
		if containsAll(string(entry), needles...) {
			return string(entry)
		}
	}
	return ""
}

func subnetID(t *testing.T, s *e2eSession, cidr string) int64 {
	t.Helper()
	list := s.must(http.MethodGet, "/api/v1/subnets", nil, http.StatusOK)
	for _, raw := range list["items"].([]any) {
		item := raw.(map[string]any)
		if item["cidr"] == cidr {
			return int64(item["id"].(float64))
		}
	}
	t.Fatalf("找不到網段 %s", cidr)
	return 0
}

// vrfStatuses 回傳指定交換器各筆 VRF 讀取設定最近一次的讀取狀態；失敗時附上原因分類與摘要。
func vrfStatuses(t *testing.T, s *e2eSession, switchID int64) map[string]string {
	t.Helper()
	sw := s.must(http.MethodGet, fmt.Sprintf("/api/v1/switches/%d", switchID), nil, http.StatusOK)
	out := map[string]string{}
	for _, raw := range sw["vrf_configs"].([]any) {
		v := raw.(map[string]any)
		status := fmt.Sprint(v["last_status"])
		if status != "success" {
			status += fmt.Sprintf("（%v：%v）", v["last_error_class"], v["last_error_summary"])
		}
		out[v["vrf_name"].(string)] = status
	}
	return out
}

// firstVRFStatus 回傳第一台交換器預設 VRF 讀取設定的狀態欄位。
func firstVRFStatus(t *testing.T, s *e2eSession) map[string]any {
	t.Helper()
	list := s.must(http.MethodGet, "/api/v1/switches", nil, http.StatusOK)
	items := list["items"].([]any)
	if len(items) == 0 {
		t.Fatal("交換器清單是空的")
	}
	vrfs := items[0].(map[string]any)["vrf_configs"].([]any)
	if len(vrfs) == 0 {
		t.Fatal("交換器沒有 VRF 讀取設定")
	}
	return vrfs[0].(map[string]any)
}

func mailSubjects(mails []simMail) []string {
	out := make([]string, 0, len(mails))
	for _, m := range mails {
		out = append(out, m.Subject)
	}
	return out
}

// assertMailFormat 檢查郵件的共通規則：multipart/alternative，純文字與 HTML 都有內容，
// HTML 不含 JavaScript，信中的連結只通往本系統的公開網址。
func assertMailFormat(t *testing.T, m simMail) {
	t.Helper()
	if !strings.HasPrefix(m.ContentType, "multipart/alternative") {
		t.Errorf("信件「%s」應為 multipart/alternative，實際 %q", m.Subject, m.ContentType)
	}
	if strings.TrimSpace(m.Text) == "" || strings.TrimSpace(m.HTML) == "" {
		t.Errorf("信件「%s」的純文字與 HTML 本文都要有內容", m.Subject)
	}
	lower := strings.ToLower(m.HTML)
	if strings.Contains(lower, "<script") || strings.Contains(lower, "javascript:") {
		t.Errorf("信件「%s」的 HTML 不可包含 JavaScript", m.Subject)
	}
	for _, body := range []string{m.Text, m.HTML} {
		for _, link := range findLinks(body) {
			if !strings.HasPrefix(link, e2ePublicBaseURL) {
				t.Errorf("信件「%s」的連結應只通往本系統：%s", m.Subject, link)
			}
		}
	}
}

// findLinks 取出文字中的 http(s) 連結。
func findLinks(body string) []string {
	var links []string
	for _, field := range strings.FieldsFunc(body, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\r' || r == '\t' || r == '"' || r == '\'' || r == '<' || r == '>' || r == '(' || r == ')'
	}) {
		if strings.HasPrefix(field, "http://") || strings.HasPrefix(field, "https://") {
			links = append(links, field)
		}
	}
	return links
}
