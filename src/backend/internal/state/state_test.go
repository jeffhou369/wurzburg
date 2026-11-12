package state

import (
	"testing"
	"time"
)

func TestComputeStateAllCombinations(t *testing.T) {
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	advanceDays := 14

	expSoon := now.Add(14 * 24 * time.Hour)
	expBoundaryNotSoon := now.Add(14*24*time.Hour + time.Second)
	expPast := now.Add(-1 * time.Second)
	expExact := now

	tests := []struct {
		name           string
		sightingsCount int
		hasOwner       bool
		expiresAt      *time.Time
		conflict       bool
		wantPresence   PresenceStatus
		wantReg        RegistrationStatus
		wantSoon       bool
		wantLabel      string
	}{
		{
			name:           "在線且未登記",
			sightingsCount: 2,
			hasOwner:       false,
			wantPresence:   PresenceOnline,
			wantReg:        RegUnregistered,
			wantLabel:      "在線 · 未登記",
		},
		{
			name:           "離線且未登記",
			sightingsCount: 0,
			hasOwner:       false,
			wantPresence:   PresenceOffline,
			wantReg:        RegUnregistered,
			wantLabel:      "離線 · 未登記",
		},
		{
			name:           "離線且已到期",
			sightingsCount: 0,
			hasOwner:       true,
			expiresAt:      &expPast,
			wantPresence:   PresenceOffline,
			wantReg:        RegExpired,
			wantLabel:      "離線 · 已到期",
		},
		{
			name:           "剛好到期時間點即為已到期",
			sightingsCount: 1,
			hasOwner:       true,
			expiresAt:      &expExact,
			wantPresence:   PresenceOnline,
			wantReg:        RegExpired,
			wantLabel:      "在線 · 已到期",
		},
		{
			name:           "剛好第 14 天邊界為即將到期",
			sightingsCount: 1,
			hasOwner:       true,
			expiresAt:      &expSoon,
			wantPresence:   PresenceOnline,
			wantReg:        RegRegistered,
			wantSoon:       true,
			wantLabel:      "在線 · 即將到期",
		},
		{
			name:           "第 14 天又多 1 秒尚未進入即將到期",
			sightingsCount: 1,
			hasOwner:       true,
			expiresAt:      &expBoundaryNotSoon,
			wantPresence:   PresenceOnline,
			wantReg:        RegRegistered,
			wantSoon:       false,
			wantLabel:      "在線 · 已登記",
		},
		{
			name:           "在線已登記且有衝突",
			sightingsCount: 1,
			hasOwner:       true,
			expiresAt:      &expBoundaryNotSoon,
			conflict:       true,
			wantPresence:   PresenceOnline,
			wantReg:        RegRegistered,
			wantLabel:      "在線 · 已登記 · 衝突",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Compute(tc.sightingsCount, tc.hasOwner, tc.expiresAt, tc.conflict, advanceDays, now)
			if got.Presence != tc.wantPresence {
				t.Errorf("presence got %s, want %s", got.Presence, tc.wantPresence)
			}
			if got.Registration != tc.wantReg {
				t.Errorf("registration got %s, want %s", got.Registration, tc.wantReg)
			}
			if got.ExpiringSoon != tc.wantSoon {
				t.Errorf("expiringSoon got %v, want %v", got.ExpiringSoon, tc.wantSoon)
			}
			if got.Label != tc.wantLabel {
				t.Errorf("label got %q, want %q", got.Label, tc.wantLabel)
			}
		})
	}
}
