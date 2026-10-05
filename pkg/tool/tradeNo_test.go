package tool

import (
	"testing"
	"time"
)

func TestGenerateTradeNoFormat(t *testing.T) {
	no := GenerateTradeNo()
	if len(no) != 22 {
		t.Fatalf("trade number %q has length %d, want fixed 22", no, len(no))
	}
	for i, c := range no {
		if c < '0' || c > '9' {
			t.Fatalf("trade number %q has non-digit %q at %d", no, c, i)
		}
	}
	if stamp, err := time.ParseInLocation("20060102150405", no[:14], time.Local); err != nil {
		t.Fatalf("trade number %q does not start with a timestamp: %v", no, err)
	} else if time.Since(stamp) > time.Minute {
		t.Fatalf("trade number timestamp %v is not recent", stamp)
	}
}

// The clock-seeded generator returned identical numbers for calls that landed
// in the same nanosecond, and trade_no is unique, so such a collision surfaced
// as a failed purchase. Two draws in the same second -- which is all a tight
// loop covers, the timestamp is second-resolution -- must not share a suffix.
//
// The sample stays at 200 on purpose: the suffix space is 10^8 and the loop
// finishes inside one second, so the birthday bound is 200*199/2/10^8 (0.02%).
// Raising n trades regression strength for a flaky suite.
func TestGenerateTradeNoRepeatsWithinTheSameSecond(t *testing.T) {
	const n = 200
	suffixes := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		no := GenerateTradeNo()
		suffix := no[len(no)-8:]
		if _, dup := suffixes[suffix]; dup {
			t.Fatalf("duplicate trade number suffix %q after %d generations", suffix, i)
		}
		suffixes[suffix] = struct{}{}
	}
}
