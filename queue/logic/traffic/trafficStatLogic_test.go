package traffic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestClearExpiredTrafficLogs(t *testing.T) {
	end := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	wantCutoff := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		autoClear bool
		clearDays int64
		wantErr   error
	}{
		{name: "disabled", autoClear: false},
		{name: "enabled", autoClear: true, clearDays: 7},
		{name: "propagates database error", autoClear: true, clearDays: 7, wantErr: errTrafficCleanup},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotCutoff time.Time
			cleaner := trafficLogCleanerFunc(func(_ context.Context, cutoff time.Time) error {
				gotCutoff = cutoff
				return tt.wantErr
			})

			err := clearExpiredTrafficLogs(context.Background(), cleaner, tt.autoClear, tt.clearDays, end)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("clearExpiredTrafficLogs() error = %v, want %v", err, tt.wantErr)
			}
			if tt.autoClear && !gotCutoff.Equal(wantCutoff) {
				t.Fatalf("cleanup cutoff = %v, want %v", gotCutoff, wantCutoff)
			}
			if !tt.autoClear && !gotCutoff.IsZero() {
				t.Fatalf("cleanup should be skipped, got cutoff %v", gotCutoff)
			}
		})
	}
}

type trafficLogCleanerFunc func(context.Context, time.Time) error

func (f trafficLogCleanerFunc) DeleteBefore(ctx context.Context, cutoff time.Time) error {
	return f(ctx, cutoff)
}

var errTrafficCleanup = errors.New("traffic cleanup failed")

func TestIsTrafficStatRetryableError(t *testing.T) {
	if !isTrafficStatRetryableError(&mysql.MySQLError{Number: 1020}) {
		t.Fatal("error 1020 should be retried")
	}
	if isTrafficStatRetryableError(&mysql.MySQLError{Number: 1213}) {
		t.Fatal("deadlock error should not be handled by the traffic-stat retry")
	}
}
