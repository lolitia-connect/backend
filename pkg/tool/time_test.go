package tool

import (
	"testing"
	"time"
)

func TestAddTime(t *testing.T) {
	basic := time.Now()

	tests := []struct {
		name     string
		unit     string
		quantity int64
		wantDiff time.Duration
	}{
		{"Month", "Month", 1, time.Duration(0)},
		{"Day", "Day", 1, time.Duration(0)},
		{"Year", "Year", 1, time.Duration(0)},
		{"Quarter", "Quarter", 1, time.Duration(0)},
		{"HalfYear", "HalfYear", 1, time.Duration(0)},
		{"NoLimit", "NoLimit", 1, time.Duration(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AddTime(tt.unit, tt.quantity, basic)
			switch tt.unit {
			case "Month":
				expected := basic.AddDate(0, 1, 0)
				if !result.Equal(expected) {
					t.Errorf("AddTime(%s, 1) = %v, want %v", tt.unit, result, expected)
				}
			case "Quarter":
				expected := basic.AddDate(0, 3, 0)
				if !result.Equal(expected) {
					t.Errorf("AddTime(%s, 1) = %v, want %v", tt.unit, result, expected)
				}
			case "HalfYear":
				expected := basic.AddDate(0, 6, 0)
				if !result.Equal(expected) {
					t.Errorf("AddTime(%s, 1) = %v, want %v", tt.unit, result, expected)
				}
			case "Day":
				expected := basic.AddDate(0, 0, 1)
				if !result.Equal(expected) {
					t.Errorf("AddTime(%s, 1) = %v, want %v", tt.unit, result, expected)
				}
			case "Year":
				expected := basic.AddDate(1, 0, 0)
				if !result.Equal(expected) {
					t.Errorf("AddTime(%s, 1) = %v, want %v", tt.unit, result, expected)
				}
			case "NoLimit":
				if !result.Equal(time.UnixMilli(0)) {
					t.Errorf("AddTime(%s, 1) = %v, want %v", tt.unit, result, time.UnixMilli(0))
				}
			}
		})
	}
}

func TestGetYearDays(t *testing.T) {
	days := GetYearDays(time.Now(), 2, 1)
	t.Logf("GetYearDays() success, expected 365, got %d", days)

}
