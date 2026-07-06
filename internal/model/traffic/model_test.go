package traffic

import (
	"testing"
	"time"
)

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
