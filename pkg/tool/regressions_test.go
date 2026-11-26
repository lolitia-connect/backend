package tool

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCalendarHelpersHandleMonthAndYearBoundaries(t *testing.T) {
	utc := time.UTC
	for _, tc := range []struct {
		name string
		date time.Time
		day  int
		want int64
	}{
		{"target already passed", time.Date(2026, 3, 20, 12, 0, 0, 0, utc), 5, 15},
		{"target in previous month", time.Date(2026, 3, 3, 12, 0, 0, 0, utc), 20, 11},
		{"january previous year", time.Date(2026, 1, 3, 12, 0, 0, 0, utc), 20, 14},
		{"clamped february", time.Date(2026, 3, 20, 12, 0, 0, 0, utc), 31, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, DaysToMonthDay(tc.date, tc.day))
		})
	}
	require.EqualValues(t, 11, DaysToNextMonth(time.Date(2026, 1, 20, 12, 0, 0, 0, utc)))
	require.EqualValues(t, 11, DaysToNextMonth(time.Date(2026, 12, 20, 12, 0, 0, 0, utc)))
	require.EqualValues(t, 28, GetLastDayOfMonth(time.Date(2026, 2, 15, 12, 0, 0, 0, utc)))
	require.Equal(t, time.Date(2024, 2, 29, 0, 0, 0, 0, utc), GetValidDate(2024, time.February, 31, utc))
	require.EqualValues(t, 365, GetYearDays(time.Date(2026, 6, 1, 12, 0, 0, 0, utc), 2, 1))
	require.EqualValues(t, 365, GetYearDays(time.Date(2024, 1, 1, 12, 0, 0, 0, utc), 2, 1))
	require.EqualValues(t, -364, DaysToYearDay(time.Date(2026, 2, 1, 12, 0, 0, 0, utc), 2, 1))
	require.EqualValues(t, -244, DaysToYearDay(time.Date(2026, 6, 1, 12, 0, 0, 0, utc), 2, 1))
	require.EqualValues(t, 13, MonthDiff(time.Date(2025, 1, 20, 0, 0, 0, 0, utc), time.Date(2026, 3, 20, 0, 0, 0, 0, utc)))
	require.EqualValues(t, 13, MonthDiff(time.Date(2025, 1, 20, 0, 0, 0, 0, utc), time.Date(2026, 3, 19, 0, 0, 0, 0, utc)))
	require.EqualValues(t, 2, YearDiff(time.Date(2024, 2, 1, 0, 0, 0, 0, utc), time.Date(2026, 2, 1, 0, 0, 0, 0, utc)))
	require.EqualValues(t, 1, YearDiff(time.Date(2024, 3, 1, 0, 0, 0, 0, utc), time.Date(2026, 2, 1, 0, 0, 0, 0, utc)))
	require.EqualValues(t, 2, DayDiff(time.Date(2026, 1, 1, 0, 0, 0, 0, utc), time.Date(2026, 1, 3, 0, 0, 0, 0, utc)))
	require.EqualValues(t, 5, HourDiff(time.Date(2026, 1, 1, 0, 0, 0, 0, utc), time.Date(2026, 1, 1, 5, 0, 0, 0, utc)))
}

func TestCollectionTemplateAndCopyHelpers(t *testing.T) {
	require.Equal(t, []int64{2, 4}, SliceIntersectInt64([]int64{1, 2, 4}, []int64{2, 4}))
	require.Equal(t, []int64{1}, SliceDifferenceInt64([]int64{1, 2, 4}, []int64{2, 4}))
	require.True(t, SliceIsExistInt64([]int64{3, 5}, 5))
	require.False(t, SliceIsExistInt64([]int64{3, 5}, 4))
	require.Equal(t, "a****z@example.test", MaskEmail("abcdez@example.test"))
	require.Equal(t, "x@example.test", MaskEmail("x@example.test"))
	require.Equal(t, "invalid", MaskEmail("invalid"))
	text, err := RenderTemplateToString("Hello {{.Name}}", map[string]string{"Name": "Ada"})
	require.NoError(t, err)
	require.Equal(t, "Hello Ada", text)
	_, err = RenderTemplateToString("{{", nil)
	require.Error(t, err)
	var shallow struct{ CreatedAt int64 }
	ShallowCopy(&shallow, struct{ CreatedAt time.Time }{CreatedAt: time.UnixMilli(1234)})
	require.EqualValues(t, 1234, shallow.CreatedAt)
	var copied struct{ CreatedAt int64 }
	require.NoError(t, CloneMapToStruct(map[string]any{"CreatedAt": int64(9)}, &copied))
	require.EqualValues(t, 9, copied.CreatedAt)
	require.Error(t, CloneMapToStruct(map[string]any{}, copied))
}
