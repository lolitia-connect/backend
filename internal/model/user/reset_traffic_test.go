package user

import (
	"testing"
	"time"
)

func TestCalendarResetCatchupAndUpgradeCompatibility(t *testing.T) {
	date := func(value string) time.Time {
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	for _, test := range []struct {
		name, start, marker, now, mode string
		want                           bool
	}{
		{"missed first", "2025-01-15", "2026-09-01", "2026-10-03", "first", true},
		{"repeat first", "2025-01-15", "2026-10-03", "2026-10-03", "first", false},
		{"upgrade away from day", "2025-01-15", "", "2026-10-03", "first", false},
		{"upgrade reset day", "2025-01-15", "", "2026-10-01", "first", true},
		{"missed two months", "2025-01-31", "2026-01-31", "2026-04-01", "monthly", true},
		{"month end", "2025-01-31", "", "2026-02-28", "monthly", true},
		{"leap anniversary", "2024-02-29", "2025-02-28", "2026-03-01", "yearly", true},
		{"leap repeat", "2024-02-29", "2026-02-28", "2026-03-01", "yearly", false},
		{"new subscription", "2026-10-03", "2026-09-01", "2026-10-04", "first", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var marker *time.Time
			if test.marker != "" {
				value := date(test.marker)
				marker = &value
			}
			if got := resetOwed(date(test.start), marker, date(test.now), test.mode); got != test.want {
				t.Fatalf("reset owed = %v, want %v", got, test.want)
			}
		})
	}
}
