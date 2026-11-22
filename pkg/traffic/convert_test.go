package traffic

import "testing"

func TestConvertSupportsDecimalAndBinaryUnits(t *testing.T) {
	cases := []struct {
		name string
		bits int64
		unit string
		want float64
	}{
		{"megabits", 2_000_000, Mb, 2},
		{"mebibytes", 2 * 1024 * 1024, MiB, 2},
		{"gigabits", 3_000_000_000, Gb, 3},
		{"gibibytes", 3 * 1024 * 1024 * 1024, GiB, 3},
		{"terabits", 4_000_000_000_000, Tb, 4},
		{"tebibytes", 4 * 1024 * 1024 * 1024 * 1024, TiB, 4},
		{"unsupported", 42, "bytes", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Convert(tc.bits, tc.unit); got != tc.want {
				t.Fatalf("Convert(%d, %q) = %v, want %v", tc.bits, tc.unit, got, tc.want)
			}
		})
	}
}

func TestAutoConvertSelectsLargestUnitAndRounding(t *testing.T) {
	cases := []struct {
		bits   int64
		binary bool
		want   string
	}{
		{999_999, false, "999999 bits"},
		{1_000_000, false, "1.00 Mb"},
		{1_000_000, true, "1000000 bits"},
		{1_572_864, true, "1.50 MiB"},
		{3 * 1024 * 1024 * 1024, true, "3.00 GiB"},
		{4 * 1024 * 1024 * 1024 * 1024, true, "4.00 TiB"},
		{2_000_000, false, "2.00 Mb"},
		{3_000_000_000, false, "3.00 Gb"},
		{4_000_000_000_000, false, "4.00 Tb"},
	}
	for _, tc := range cases {
		if got := AutoConvert(tc.bits, tc.binary); got != tc.want {
			t.Errorf("AutoConvert(%d, %t) = %q, want %q", tc.bits, tc.binary, got, tc.want)
		}
	}
}
