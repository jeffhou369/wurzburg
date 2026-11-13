package interfaces

import (
	"context"
	"sync"
)

// ========================================================
// 測試替身實作（Fakes）供各平行功能測試環境使用
// ========================================================

// FakeCollector 可指定針對不同 VRFConfigID 回傳固定輸出或錯誤。
type FakeCollector struct {
	mu      sync.Mutex
	Outputs map[int64]*PollOutput
	Errors  map[int64]error
	Calls   []VRFReadInput
}

func NewFakeCollector() *FakeCollector {
	return &FakeCollector{
		Outputs: make(map[int64]*PollOutput),
		Errors:  make(map[int64]error),
	}
}

func (f *FakeCollector) Collect(ctx context.Context, input VRFReadInput) (*PollOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, input)
	if err, ok := f.Errors[input.VRFConfigID]; ok {
		return nil, err
	}
	if out, ok := f.Outputs[input.VRFConfigID]; ok {
		return out, nil
	}
	return &PollOutput{
		VRFConfigID: input.VRFConfigID,
		SwitchID:    input.SwitchID,
		VRFName:     input.VRFName,
	}, nil
}

// FakeAuthSource 模擬 AD 或 RADIUS 帳密驗證結果。
type FakeAuthSource struct {
	Users   map[string]string // username -> expected password
	Info    map[string]*OrgUserInfo
	FailErr error
}

func NewFakeAuthSource() *FakeAuthSource {
	return &FakeAuthSource{
		Users: make(map[string]string),
		Info:  make(map[string]*OrgUserInfo),
	}
}

func (f *FakeAuthSource) Authenticate(ctx context.Context, username, password string) (*OrgUserInfo, error) {
	if f.FailErr != nil {
		return nil, f.FailErr
	}
	expectedPass, ok := f.Users[username]
	if !ok || expectedPass != password {
		return nil, ErrInvalidCredentials
	}
	if info, exists := f.Info[username]; exists {
		return info, nil
	}
	return &OrgUserInfo{
		Username:    username,
		DisplayName: username,
		AuthSource:  "ad",
	}, nil
}

// FakeOUILookup 預設回傳「未知」，可註冊特定 MAC 前綴之廠商名稱。
type FakeOUILookup struct {
	Vendors map[string]string
}

func NewFakeOUILookup() *FakeOUILookup {
	return &FakeOUILookup{Vendors: make(map[string]string)}
}

func (f *FakeOUILookup) Lookup(mac string) string {
	if len(mac) >= 8 {
		if v, ok := f.Vendors[mac[:8]]; ok {
			return v
		}
	}
	return "未知"
}

// FakeMailer 攔截所有寄出之郵件並保存於 Sent 中供測試檢查，或模擬寄送失敗。
type FakeMailer struct {
	mu      sync.Mutex
	Sent    []MailMessage
	FailErr error
	// OnSend 若有設定，每次呼叫 Send 時都會收到訊息（含模擬寄送失敗的），不受 Reset／PopAll 影響。
	// 供測試在寄出當下檢查每一封信。
	OnSend func(MailMessage)
}

func NewFakeMailer() *FakeMailer {
	return &FakeMailer{}
}

func (f *FakeMailer) Send(ctx context.Context, msg MailMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.OnSend != nil {
		f.OnSend(msg)
	}
	if f.FailErr != nil {
		return f.FailErr
	}
	f.Sent = append(f.Sent, msg)
	return nil
}

func (f *FakeMailer) SentMessages() []MailMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]MailMessage, len(f.Sent))
	copy(out, f.Sent)
	return out
}

func (f *FakeMailer) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Sent = nil
	f.FailErr = nil
}

func (f *FakeMailer) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Sent)
}

func (f *FakeMailer) PopAll() []MailMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]MailMessage, len(f.Sent))
	copy(out, f.Sent)
	f.Sent = nil
	return out
}
