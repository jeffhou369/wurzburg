package state

import (
	"testing"
	"time"
)

func TestComputeStateAllCombinations(t *testing.T) {
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	advanceDays := 14

	expSoon := now.Add(14 * 24 * time.Hour)
	// 「即將到期」以日期判斷：到期日在 15 天後的第一秒，才是第一個不算即將到期的時間點
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	expBoundaryNotSoon := today.AddDate(0, 0, 15)
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
			name:           "到期日在 15 天後尚未進入即將到期",
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
			got := Compute(tc.sightingsCount, tc.hasOwner, tc.expiresAt, tc.conflict, advanceDays, now, time.UTC)
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

// 「即將到期」以園區時區的日期判斷，與到期催認信相同：到期日距離今天不超過提前天數就算。
// 催認信在到期日前 14 天的早上寄出；若改以精確時間判斷，下午才到期的登記在當天早上還不算即將到期，
// 擁有者依信中連結打開清單會是空的。
func TestExpiringSoonFollowsCampusCalendarDays(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Date(2027, 11, 16, 11, 5, 0, 0, loc) // 到期日 11/16，上午 11:05 到期
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"到期日前 15 天的深夜", time.Date(2027, 11, 1, 23, 59, 0, 0, loc), false},
		{"到期日前 14 天剛過午夜", time.Date(2027, 11, 2, 0, 0, 0, 0, loc), true},
		{"到期日前 14 天早上寄催認信的時間", time.Date(2027, 11, 2, 8, 0, 0, 0, loc), true},
		{"到期日當天、到期時間之前", time.Date(2027, 11, 16, 9, 0, 0, 0, loc), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Compute(1, true, &expires, false, 14, tc.now.UTC(), loc)
			if got.ExpiringSoon != tc.want {
				t.Errorf("ExpiringSoon 得到 %v，預期 %v", got.ExpiringSoon, tc.want)
			}
			if got.Registration != RegRegistered {
				t.Errorf("尚未到期的登記應為已登記，實際 %s", got.Registration)
			}
			if before := ExpiringSoonBefore(tc.now.UTC(), loc, 14); expires.Before(before) != tc.want {
				t.Errorf("ExpiringSoonBefore 的界線與 Compute 不一致：%s", before.In(loc))
			}
		})
	}

	// 時區不同，同一個時間點落在不同的日期：以園區時區為準
	utcEvening := time.Date(2027, 11, 1, 16, 30, 0, 0, time.UTC) // 台北已是 11/2 00:30
	if got := Compute(1, true, &expires, false, 14, utcEvening, loc); !got.ExpiringSoon {
		t.Error("園區時區已進入到期日前 14 天時應算即將到期")
	}
	if got := Compute(1, true, &expires, false, 14, utcEvening, time.UTC); got.ExpiringSoon {
		t.Error("以 UTC 計算時同一個時間點還在到期日前 15 天")
	}
}
