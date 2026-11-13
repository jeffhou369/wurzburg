package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// 郵件模擬伺服器：以 Testcontainers 啟動 Mailpit 收信，讓真正的 SMTP 寄信模組（net/smtp）連線、驗證、寄送。
// 測試經由 Mailpit 的 HTTP API 讀取收到的信，並可注入 SMTP 錯誤來模擬郵件伺服器暫時拒絕。
//
// 連線不加密（smtp_tls_mode 為 none）：模擬伺服器只在本機迴路上，net/smtp 允許對 127.0.0.1 以明文做 PLAIN 驗證。
// STARTTLS 與 SMTPS 需要受信任的憑證，不在模擬範圍。

// mailSimImage 鎖定映像的 digest。對應 axllent/mailpit:latest（2026-11-07 建置）。
const mailSimImage = "axllent/mailpit@sha256:aef2aacc11f075ec0fb6874275ee4a421e688e11a96ebb296134d1735c7761ce"

// SMTP 驗證帳密；模擬伺服器只接受這一組。
const (
	mailSimUser     = "mailer-bot"
	mailSimPassword = "SimSmtpPass#41"
)

type mailSim struct {
	container testcontainers.Container
	host      string
	smtpPort  int
	apiBase   string
}

// simMail 是模擬伺服器收到的一封信。
type simMail struct {
	ID          string
	From        string
	To          []string
	Cc          []string
	Subject     string
	Text        string
	HTML        string
	ContentType string // 最外層的 Content-Type 標頭
}

func startMailSim(t *testing.T) *mailSim {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        mailSimImage,
			ExposedPorts: []string{"1025/tcp", "8025/tcp"},
			Cmd:          []string{"--smtp-auth-file", "/sim/smtp-auth", "--smtp-auth-allow-insecure", "--enable-chaos"},
			Files: []testcontainers.ContainerFile{
				{Reader: strings.NewReader(mailSimUser + ":" + mailSimPassword + "\n"), ContainerFilePath: "/sim/smtp-auth", FileMode: 0o644},
			},
			WaitingFor: wait.ForAll(
				wait.ForListeningPort("1025/tcp"),
				wait.ForHTTP("/api/v1/info").WithPort("8025/tcp"),
			).WithDeadline(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("啟動郵件模擬伺服器失敗: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })

	smtpPort, err := c.MappedPort(ctx, "1025/tcp")
	if err != nil {
		t.Fatalf("取得郵件模擬伺服器的 SMTP 埠失敗: %v", err)
	}
	apiPort, err := c.MappedPort(ctx, "8025/tcp")
	if err != nil {
		t.Fatalf("取得郵件模擬伺服器的 API 埠失敗: %v", err)
	}
	return &mailSim{
		container: c, host: "127.0.0.1", smtpPort: int(smtpPort.Num()),
		apiBase: fmt.Sprintf("http://127.0.0.1:%d", apiPort.Num()),
	}
}

func (m *mailSim) api(t *testing.T, method, path string, body any, out any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, m.apiBase+path, reader)
	if err != nil {
		t.Fatalf("建立郵件模擬伺服器請求失敗: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("呼叫郵件模擬伺服器 %s %s 失敗: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		t.Fatalf("郵件模擬伺服器 %s %s 回應 %d：%s", method, path, resp.StatusCode, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("解析郵件模擬伺服器回應失敗: %v（%s）", err, raw)
		}
	}
}

type mailpitAddress struct{ Address string }

func addresses(list []mailpitAddress) []string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.Address)
	}
	return out
}

// received 回傳模擬伺服器目前收到的全部信件（由舊到新），含兩種本文。每一封都經過 Secret 掃描。
func (m *mailSim) received(t *testing.T) []simMail {
	t.Helper()
	var list struct {
		Messages []struct{ ID string }
	}
	m.api(t, http.MethodGet, "/api/v1/messages?limit=500", nil, &list)
	out := make([]simMail, 0, len(list.Messages))
	for i := len(list.Messages) - 1; i >= 0; i-- { // API 由新到舊排列
		id := list.Messages[i].ID
		var detail struct {
			From    mailpitAddress
			To      []mailpitAddress
			Cc      []mailpitAddress
			Subject string
			Text    string
			HTML    string
		}
		m.api(t, http.MethodGet, "/api/v1/message/"+id, nil, &detail)
		var headers map[string][]string
		m.api(t, http.MethodGet, "/api/v1/message/"+id+"/headers", nil, &headers)
		mail := simMail{
			ID: id, From: detail.From.Address, To: addresses(detail.To), Cc: addresses(detail.Cc),
			Subject: detail.Subject, Text: detail.Text, HTML: detail.HTML,
		}
		if ct := headers["Content-Type"]; len(ct) > 0 {
			mail.ContentType = ct[0]
		}
		scanSentMailParts(t, mail.To, mail.Cc, mail.Subject, mail.Text, mail.HTML)
		out = append(out, mail)
	}
	return out
}

// takeAll 回傳目前收到的全部信件並清空信箱，讓下一個情境只看到自己觸發的信。
func (m *mailSim) takeAll(t *testing.T) []simMail {
	t.Helper()
	mails := m.received(t)
	m.api(t, http.MethodDelete, "/api/v1/messages", nil, nil)
	return mails
}

// rejectRecipients 讓模擬伺服器以指定的 SMTP 錯誤碼拒絕所有收件者（RCPT TO），模擬郵件伺服器暫時無法收信。
func (m *mailSim) rejectRecipients(t *testing.T, smtpCode int) {
	t.Helper()
	m.api(t, http.MethodPut, "/api/v1/chaos", map[string]any{
		"Recipient": map[string]any{"ErrorCode": smtpCode, "Probability": 100},
	}, nil)
}

// acceptAll 取消錯誤注入，恢復正常收信。
func (m *mailSim) acceptAll(t *testing.T) {
	t.Helper()
	m.api(t, http.MethodPut, "/api/v1/chaos", map[string]any{
		"Recipient": map[string]any{"ErrorCode": 451, "Probability": 0},
	}, nil)
}

// stop 終止郵件模擬伺服器，模擬郵件伺服器中斷。
func (m *mailSim) stop(t *testing.T) {
	t.Helper()
	if err := testcontainers.TerminateContainer(m.container); err != nil {
		t.Fatalf("終止郵件模擬伺服器失敗: %v", err)
	}
}
