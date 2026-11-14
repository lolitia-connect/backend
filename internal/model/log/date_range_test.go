package log

import "testing"

func TestResolveDateRange(t *testing.T) {
	cases := []struct {
		name  string
		param *FilterParams
		mode  DateRangeMode
		start string
		end   string
	}{
		{"nil", nil, DateRangeNone, "", ""},
		{"empty", &FilterParams{}, DateRangeNone, "", ""},
		{"exact only", &FilterParams{Data: "2026-01-02"}, DateRangeExact, "2026-01-02", ""},
		{"from only", &FilterParams{StartDate: "2026-01-01"}, DateRangeFrom, "2026-01-01", ""},
		{"to only", &FilterParams{EndDate: "2026-01-31"}, DateRangeTo, "", "2026-01-31"},
		{
			"between",
			&FilterParams{StartDate: "2026-01-01", EndDate: "2026-01-31"},
			DateRangeBetween, "2026-01-01", "2026-01-31",
		},
		{
			// An explicit range must win over a stale exact date so the query is
			// deterministic when a client sends both.
			"range beats exact",
			&FilterParams{Data: "2026-03-03", StartDate: "2026-01-01", EndDate: "2026-01-31"},
			DateRangeBetween, "2026-01-01", "2026-01-31",
		},
		{
			"start beats exact",
			&FilterParams{Data: "2026-03-03", StartDate: "2026-01-01"},
			DateRangeFrom, "2026-01-01", "",
		},
		{
			"end beats exact",
			&FilterParams{Data: "2026-03-03", EndDate: "2026-01-31"},
			DateRangeTo, "", "2026-01-31",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mode, start, end := c.param.ResolveDateRange()
			if mode != c.mode || start != c.start || end != c.end {
				t.Fatalf("ResolveDateRange() = (%d, %q, %q), want (%d, %q, %q)",
					mode, start, end, c.mode, c.start, c.end)
			}
		})
	}
}
