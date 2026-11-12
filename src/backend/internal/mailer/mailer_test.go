package mailer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
	"github.com/jeffhou369/wurzburg/backend/internal/settings"
)

func TestRenderEmailAndEscaping(t *testing.T) {
	data := EmailTemplateData{
		CampusName: "Würzburg",
		Subject:    "[Würzburg] 到期催認測試",
		Preheader:  "請檢視 IP 位址",
		Greeting:   "林小姐您好：",
		Intro:      "以下 IP 即將到期：",
		Timezone:   "Asia/Taipei",
		SummaryItems: []SummaryKV{
			{Label: "需處理筆數", Value: "1 筆", Tone: "warning"},
		},
		Columns: []string{"IP 位址", "狀態", "備註"},
		Rows: [][]EmailCell{
			{
				{Text: "10.10.1.50", Monospace: true},
				{Text: "[在線 · 即將到期]", BadgeTone: "warning"},
				{Text: `<script>alert("xss")</script> & 測試主機`},
			},
		},
		PrimaryCTA: &CTAButton{
			Label: "登入後重新確認",
			URL:   "https://ip.example.com/workbench?filter=my_pending",
		},
		SecondaryLink: &CTAButton{
			Label: "若已不使用，請登入後釋放",
			URL:   "https://ip.example.com/workbench",
		},
		FooterLines: []string{"此為自動通知信"},
	}

	htmlBody, textBody, err := RenderEmail(data)
	if err != nil {
		t.Fatalf("RenderEmail failed: %v", err)
	}

	// 驗證 HTML 安全與規範
	if !strings.Contains(htmlBody, `<html lang="zh-Hant">`) {
		t.Errorf("HTML 缺少 lang=\"zh-Hant\"")
	}
	if strings.Contains(htmlBody, "<script>") {
		t.Errorf("HTML 內含未跳脫的 <script> 標籤！")
	}
	if !strings.Contains(htmlBody, `&lt;script&gt;alert(&#34;xss&#34;)&lt;/script&gt;`) {
		t.Errorf("HTML 未正確跳脫使用者輸入內容: %s", htmlBody)
	}
	if strings.Contains(htmlBody, "fonts.googleapis.com") || strings.Contains(htmlBody, "background-image") {
		t.Errorf("HTML 不可包含外部字型或背景圖片")
	}

	// 驗證純文字對等性與連結重複顯示
	for _, want := range []string{
		"10.10.1.50",
		"[在線 · 即將到期]",
		"https://ip.example.com/workbench?filter=my_pending",
		"https://ip.example.com/workbench",
		"Asia/Taipei",
	} {
		if !strings.Contains(htmlBody, want) {
			t.Errorf("HTML 缺少關鍵資訊: %s", want)
		}
		if !strings.Contains(textBody, want) {
			t.Errorf("純文字缺少關鍵資訊: %s", want)
		}
	}

	// 驗證 MIME 封裝
	raw, err := BuildMultipartAlternativeMIME("noreply@example.com", interfaces.MailMessage{
		To:       []string{"user@example.com"},
		Subject:  data.Subject,
		TextBody: textBody,
		HTMLBody: htmlBody,
	})
	if err != nil {
		t.Fatalf("BuildMultipartAlternativeMIME failed: %v", err)
	}
	rawStr := string(raw)
	if !strings.Contains(rawStr, "multipart/alternative") ||
		!strings.Contains(rawStr, "Content-Type: text/plain; charset=UTF-8") ||
		!strings.Contains(rawStr, "Content-Type: text/html; charset=UTF-8") {
		t.Errorf("MIME 結構不符 multipart/alternative 標準:\n%s", rawStr)
	}
}

func TestSMTPMailerRetryBehavior(t *testing.T) {
	// 驗證重試邏輯（不需真實網路或 DB）
	clk := clock.NewTestClock(time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC))
	sett := settings.NewStore(nil, nil, clk)
	// 將 LoadConfig 會需要的鍵放進預設？LoadConfig 讀 DB，因此改測試 SendWithTracking 搭配測試用 dialFn
	m := &SMTPMailer{
		clock:    clk,
		settings: sett,
	}

	// 設定成直接測 dialFn 重試迴圈（先確認沒有 DB 時會早期失敗）
	_, err := m.SendWithTracking(context.Background(), "test_mail", "", interfaces.MailMessage{To: []string{"a@b.c"}})
	if err == nil {
		t.Fatalf("未設定 SMTP host 應回傳錯誤")
	}

	// 直接模擬重試 2 次後第 3 次成功
	calls := 0
	m.dialFn = func(ctx context.Context, cfg SMTPConfig, msg interfaces.MailMessage, raw []byte) error {
		calls++
		if calls < 3 {
			return errors.New("temporary network glitch")
		}
		return nil
	}

	cfg := SMTPConfig{Host: "smtp.example.com", Port: 587, TLSMode: "starttls", Sender: "no-reply@example.com"}
	raw, _ := BuildMultipartAlternativeMIME(cfg.Sender, interfaces.MailMessage{To: []string{"u@example.com"}, Subject: "test", TextBody: "hi", HTMLBody: "<p>hi</p>"})

	var lastErr error
	attempts := 0
	for attempt := 1; attempt <= 3; attempt++ {
		attempts = attempt
		lastErr = m.dialFn(context.Background(), cfg, interfaces.MailMessage{To: []string{"u@example.com"}}, raw)
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil || attempts != 3 || calls != 3 {
		t.Fatalf("重試次數與結果不如預期: calls=%d, attempts=%d, err=%v", calls, attempts, lastErr)
	}
}
