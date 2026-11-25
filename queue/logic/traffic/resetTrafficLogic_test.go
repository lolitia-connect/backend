package traffic

import (
	"errors"
	"testing"
	"time"
)

func TestResetTrafficRetryClassificationAndCounters(t *testing.T) {
	service, _ := trafficStatTestService(t)
	logic := NewResetTrafficLogic(service)
	for _, test := range []struct {
		message string
		want    bool
	}{
		{"connection refused", true}, {"database is locked", true}, {"service unavailable", true},
		{"foreign key constraint", false}, {"permission denied", false}, {"authentication failed", false},
		{"unexpected provider response", true},
	} {
		if got := logic.isRetryableError(errors.New(test.message)); got != test.want {
			t.Fatalf("isRetryableError(%q) = %t, want %t", test.message, got, test.want)
		}
	}
	if logic.isRetryableError(nil) {
		t.Fatal("nil error was classified as retryable")
	}
	ctx := t.Context()
	if got := logic.getRetryCount(ctx); got != 0 {
		t.Fatalf("missing retry count = %d", got)
	}
	if err := service.Redis.Set(ctx, retryCountKey, "not-a-number", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if got := logic.getRetryCount(ctx); got != 0 {
		t.Fatalf("malformed retry count = %d", got)
	}
	logic.setRetryCount(ctx, 2)
	if got := logic.getRetryCount(ctx); got != 2 {
		t.Fatalf("stored retry count = %d", got)
	}
	logic.clearRetryCount(ctx)
	if got := logic.getRetryCount(ctx); got != 0 {
		t.Fatalf("cleared retry count = %d", got)
	}
}
func TestFirstDayResetAlreadyProcessed(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 30, 0, 0, time.Local)

	tests := []struct {
		name  string
		cache resetTrafficCache
		want  bool
	}{
		{
			name:  "missing cache should not skip",
			cache: resetTrafficCache{},
			want:  false,
		},
		{
			name: "same month should skip",
			cache: resetTrafficCache{
				LastResetTime: time.Date(2026, 6, 1, 0, 31, 0, 0, time.Local),
			},
			want: true,
		},
		{
			name: "previous month should not skip",
			cache: resetTrafficCache{
				LastResetTime: time.Date(2026, 5, 31, 23, 50, 0, 0, time.Local),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstDayResetAlreadyProcessed(now, tt.cache); got != tt.want {
				t.Fatalf("firstDayResetAlreadyProcessed() = %v, want %v", got, tt.want)
			}
		})
	}
}
