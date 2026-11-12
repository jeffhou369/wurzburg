package interfaces

import (
	"context"
	"errors"
	"time"
)

// ========================================================
// 1. SNMP 採集模組介面
// ========================================================

// ErrorClass 定義採集失敗的分類。
type ErrorClass string

const (
	ErrClassTimeout           ErrorClass = "timeout"
	ErrClassAuthFailed        ErrorClass = "auth_failed"
	ErrClassConnectionRefused ErrorClass = "connection_refused"
	ErrClassNoData            ErrorClass = "no_data"
)

// CollectError 攜帶標準化失敗類別的採集錯誤。
type CollectError struct {
	Class   ErrorClass
	Summary string
	Cause   error
}

func (e *CollectError) Error() string {
	return e.Summary
}

func (e *CollectError) Unwrap() error {
	return e.Cause
}

// VRFReadInput 代表單筆待採集的 VRF 讀取設定。
type VRFReadInput struct {
	SwitchID      int64
	VRFConfigID   int64
	MgmtIP        string
	Platform      string // ios-xe | nx-os
	SNMPVersion   string // v2c | v3
	SNMPPort      int
	TimeoutMs     int
	VRFName       string
	SNMPContext   string
	SNMPCommunity string
	V3Username    string
	V3AuthProto   string
	V3AuthPass    string
	V3PrivProto   string
	V3PrivPass    string
}

// ARPObservation 代表單一筆 ARP 觀測輸出。
type ARPObservation struct {
	IPAddress  string
	MACAddress string
	IfIndex    int64
	IfName     string
	ARPType    string // dynamic | static | other
}

// InterfaceAddress 代表單一筆交換器介面位址輸出。
type InterfaceAddress struct {
	IPAddress string
	NetMask   string
	IfIndex   int64
	IfName    string
}

// PollOutput 定義採集模組對系統其他部分唯一輸出的三樣資料。
type PollOutput struct {
	VRFConfigID        int64
	SwitchID           int64
	VRFName            string
	CollectedAt        time.Time
	ARPObservations    []ARPObservation
	InterfaceAddresses []InterfaceAddress
	InterfaceNames     map[int64]string
}

// Collector 為 SNMP 採集模組介面。
type Collector interface {
	Collect(ctx context.Context, input VRFReadInput) (*PollOutput, error)
}

// ========================================================
// 2. 驗證來源模組介面 (AD / RADIUS)
// ========================================================

var (
	ErrInvalidCredentials = errors.New("帳號或密碼錯誤")
	ErrGroupRestricted    = errors.New("此帳號不在允許登入之群組中")
	ErrAuthUnavailable    = errors.New("無法連線至驗證伺服器")
)

// OrgUserInfo 定義外部驗證成功後回傳之帳號基本資訊。
type OrgUserInfo struct {
	Username    string
	DisplayName string
	Mail        string
	AuthSource  string // ad | radius
}

// AuthSource 為一般使用者外部驗證介面。
type AuthSource interface {
	Authenticate(ctx context.Context, username, password string) (*OrgUserInfo, error)
}

// ========================================================
// 3. 本地 OUI 查詢介面
// ========================================================

// OUILookup 定義本地 MAC 廠商查詢介面，找不到時一律回傳「未知」。
type OUILookup interface {
	Lookup(mac string) string
}

// ========================================================
// 4. 郵件寄送介面
// ========================================================

// MailMessage 代表一封必須同時具備純文字與 HTML 內容的通知郵件。
type MailMessage struct {
	To       []string
	Cc       []string
	Subject  string
	TextBody string
	HTMLBody string
}

// Mailer 定義 SMTP 郵件發送介面。
type Mailer interface {
	Send(ctx context.Context, msg MailMessage) error
}
