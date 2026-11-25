package traffic

import (
	"testing"
	"time"
)

func TestDailyRankingsUseLocalMidnightAcrossClockChanges(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, date := range []time.Time{
		time.Date(2025, time.March, 9, 12, 0, 0, 0, loc),
		time.Date(2025, time.November, 2, 12, 0, 0, 0, loc),
	} {
		start, end := dayRange(date)
		midnight := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
		if !start.Equal(midnight) || !end.Equal(midnight.AddDate(0, 0, 1)) {
			t.Errorf("daily ranking for %s spans %s to %s", date.Format(time.DateOnly), start, end)
		}
	}
}

func TestTrafficRanges(t *testing.T) {
	date := time.Date(2026, 5, 22, 13, 14, 15, 0, time.UTC)
	start, end := dayRange(date)
	if !start.Equal(time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected day start: %v", start)
	}
	if !end.Equal(time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected day end: %v", end)
	}

	start, end = monthRange(date)
	if !start.Equal(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected month start: %v", start)
	}
	if !end.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected month end: %v", end)
	}
}
