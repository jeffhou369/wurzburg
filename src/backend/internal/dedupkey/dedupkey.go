// Package dedupkey 集中定義通知去重鍵（notification_states.dedup_key）的格式。
//
// 去重鍵由寄信端建立、由刪除 IP 紀錄或異動登記的模組清除；兩邊必須使用同一種格式，
// 否則會留下孤兒去重鍵或誤刪其他 IP 的狀態。所有模組都應透過本套件建立與比對，不要自行組字串。
//
// 格式：
//   - 未登記：unreg|<ip>|<mac>
//   - 每週總表：weekly|<ISO 年>-W<兩位數週次>
//   - 到期催認：expiry|<ip>|<到期日 YYYY-MM-DD>|<催認輪次>
package dedupkey

import (
	"fmt"
	"strings"
)

const (
	unregisteredKind = "unreg"
	weeklyKind       = "weekly"
	expiryKind       = "expiry"
	sep              = "|"
)

// Unregistered 回傳某 IP 與 MAC 組合的未登記通知去重鍵。
func Unregistered(ip, mac string) string {
	return unregisteredKind + sep + ip + sep + mac
}

// Weekly 回傳某 ISO 週的每週總表去重鍵。
func Weekly(isoYear, isoWeek int) string {
	return fmt.Sprintf("%s%s%d-W%02d", weeklyKind, sep, isoYear, isoWeek)
}

// Expiry 回傳某 IP 在某到期日、某催認輪次的到期催認去重鍵。
func Expiry(ip, expiryDate string, round int) string {
	return fmt.Sprintf("%s%s%s%s%s%s%d", expiryKind, sep, ip, sep, expiryDate, sep, round)
}

// UnregisteredSQL 回傳以 SQL 欄位組出未登記去重鍵的 CONCAT 運算式，供 JOIN 比對使用。
// 參數必須是程式內固定的欄位名稱，不可傳入使用者輸入。
func UnregisteredSQL(ipColumn, macColumn string) string {
	return "CONCAT('" + unregisteredKind + sep + "', " + ipColumn + ", '" + sep + "', " + macColumn + ")"
}

// UnregisteredPattern 回傳比對某 IP 所有未登記去重鍵的 LIKE 樣式（不論 MAC），已跳脫萬用字元。
func UnregisteredPattern(ip string) string {
	return unregisteredKind + sep + likeEscape(ip) + sep + "%"
}

// ExpiryPattern 回傳比對某 IP 所有到期催認去重鍵的 LIKE 樣式（不論到期日與輪次），已跳脫萬用字元。
func ExpiryPattern(ip string) string {
	return expiryKind + sep + likeEscape(ip) + sep + "%"
}

// likeEscape 跳脫 LIKE 的萬用字元與跳脫字元（MariaDB 預設跳脫字元為反斜線），確保樣式只比對到該 IP。
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
