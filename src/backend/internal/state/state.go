package state

import (
	"strings"
	"time"
)

// PresenceStatus 代表在線狀態。
type PresenceStatus string

const (
	PresenceOnline  PresenceStatus = "online"
	PresenceOffline PresenceStatus = "offline"
)

// RegistrationStatus 代表登記狀態。
type RegistrationStatus string

const (
	RegUnregistered RegistrationStatus = "unregistered"
	RegRegistered   RegistrationStatus = "registered"
	RegExpired      RegistrationStatus = "expired"
)

// ComputedState 封裝單筆 IP 紀錄由統一規則推算出的所有狀態與文字標籤。
type ComputedState struct {
	Presence     PresenceStatus
	Registration RegistrationStatus
	ExpiringSoon bool
	HasConflict  bool
	Label        string
}

// ExpiringSoonBefore 回傳「即將到期」的界線：到期時間早於這個時間點、且尚未到期的登記，視為即將到期。
//
// 以園區時區的日期計算：到期日距離今天不超過 advanceDays 天就算，界線是「今天加 advanceDays 天」那一天的結束。
// 到期催認信也是依日期在到期日前 advanceDays 天寄出；清單篩選、狀態標示與催認信必須用同一條界線，
// 否則擁有者收到信之後，依信中連結打開的待確認清單可能還看不到那筆登記。
// loc 為 nil 時以 UTC 計算。
func ExpiringSoonBefore(now time.Time, loc *time.Location, advanceDays int) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	startOfToday := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return startOfToday.AddDate(0, 0, advanceDays+1)
}

// Compute 依循開發規格第 3 節之規則，根據基礎欄位推算完整顯示狀態。
// loc 是園區時區，用來判斷「即將到期」（見 ExpiringSoonBefore）。
func Compute(
	sightingsCount int,
	hasOwner bool,
	expiresAt *time.Time,
	hasActiveConflict bool,
	advanceDays int,
	now time.Time,
	loc *time.Location,
) ComputedState {
	presence := PresenceOffline
	if sightingsCount > 0 {
		presence = PresenceOnline
	}

	reg := RegUnregistered
	expiringSoon := false

	if hasOwner {
		if expiresAt != nil && !now.Before(*expiresAt) {
			// 已到達或超過到期時間 -> 已到期
			reg = RegExpired
		} else {
			reg = RegRegistered
			if expiresAt != nil && advanceDays > 0 && expiresAt.Before(ExpiringSoonBefore(now, loc, advanceDays)) {
				expiringSoon = true
			}
		}
	}

	var parts []string
	if presence == PresenceOnline {
		parts = append(parts, "在線")
	} else {
		parts = append(parts, "離線")
	}

	switch reg {
	case RegUnregistered:
		parts = append(parts, "未登記")
	case RegExpired:
		parts = append(parts, "已到期")
	case RegRegistered:
		if expiringSoon {
			parts = append(parts, "即將到期")
		} else {
			parts = append(parts, "已登記")
		}
	}

	if hasActiveConflict {
		parts = append(parts, "衝突")
	}

	return ComputedState{
		Presence:     presence,
		Registration: reg,
		ExpiringSoon: expiringSoon,
		HasConflict:  hasActiveConflict,
		Label:        strings.Join(parts, " · "),
	}
}
