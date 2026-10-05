package order

import (
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/model/coupon"
)

// Coupon start/expire times are stored as Unix milliseconds. Comparing them
// against a seconds clock made every coupon with a start time permanently
// "not active" (50003), which broke every coupon purchase.
func TestEnsureCouponEnabledUsesMillisecondTimestamps(t *testing.T) {
	enabled := true
	now := time.Now().UnixMilli()
	hour := time.Hour.Milliseconds()

	tests := []struct {
		name    string
		start   int64
		expire  int64
		wantErr string
	}{
		{name: "inside window is accepted", start: now - hour, expire: now + 365*24*hour, wantErr: ""},
		{name: "not yet started is rejected", start: now + hour, expire: now + 2*hour, wantErr: "not active"},
		{name: "expired is rejected", start: now - 2*hour, expire: now - hour, wantErr: "expired"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ensureCouponEnabled(&coupon.Coupon{
				Enable:     &enabled,
				StartTime:  tt.start,
				ExpireTime: tt.expire,
			})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ensureCouponEnabled error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ensureCouponEnabled error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// A coupon without an expiry used to be accepted forever; the gate now treats a
// missing expiry as unusable rather than as an unlimited coupon.
func TestEnsureCouponEnabledRejectsMissingExpiry(t *testing.T) {
	enabled := true
	if err := ensureCouponEnabled(&coupon.Coupon{Enable: &enabled, ExpireTime: 0}); err == nil {
		t.Fatal("ensureCouponEnabled error = nil, want an expired-coupon error")
	}
}

func TestEnsureCouponEnabledRejectsDisabled(t *testing.T) {
	disabled := false
	now := time.Now().UnixMilli()
	err := ensureCouponEnabled(&coupon.Coupon{
		Enable:     &disabled,
		StartTime:  now - 1000,
		ExpireTime: now + 1000,
	})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("ensureCouponEnabled error = %v, want containing %q", err, "disabled")
	}
}

// A nil coupon is rejected rather than dereferenced.
func TestEnsureCouponEnabledRejectsNil(t *testing.T) {
	if err := ensureCouponEnabled(nil); err == nil {
		t.Fatal("ensureCouponEnabled(nil) error = nil, want an error")
	}
}
