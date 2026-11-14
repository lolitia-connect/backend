package orderLogic

import "testing"

func TestCalculateCommission(t *testing.T) {
	l := &ActivateOrderLogic{}
	cases := []struct {
		name       string
		price      int64
		percentage uint8
		want       int64
	}{
		{"zero percentage", 10000, 0, 0},
		{"ten percent", 10000, 10, 1000},
		{"fractional truncates", 9999, 10, 999},
		{"hundred percent", 500, 100, 500},
		{"zero price", 0, 30, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := l.calculateCommission(c.price, c.percentage); got != c.want {
				t.Fatalf("calculateCommission(%d, %d) = %d, want %d", c.price, c.percentage, got, c.want)
			}
		})
	}
}

// The commission handler skips processing when the computed amount is not
// positive, so these values must never produce a log entry.
func TestCalculateCommissionNonPositiveTriggersSkip(t *testing.T) {
	l := &ActivateOrderLogic{}
	if amount := l.calculateCommission(0, 50); amount > 0 {
		t.Fatalf("zero-price order must not accrue commission, got %d", amount)
	}
	if amount := l.calculateCommission(-100, 50); amount > 0 {
		t.Fatalf("negative-price order must not accrue commission, got %d", amount)
	}
}
