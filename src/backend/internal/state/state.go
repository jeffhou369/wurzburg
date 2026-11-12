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

// Compute 依循開發規格第 3 節之規則，根據基礎欄位推算完整顯示狀態。
func Compute(
	sightingsCount int,
	hasOwner bool,
	expiresAt *time.Time,
	hasActiveConflict bool,
	advanceDays int,
	now time.Time,
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
			if expiresAt != nil && advanceDays > 0 {
				threshold := now.Add(time.Duration(advanceDays) * 24 * time.Hour)
				if !expiresAt.After(threshold) {
					expiringSoon = true
				}
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
