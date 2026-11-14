package order

import (
	"time"

	"github.com/perfect-panel/server/internal/model/coupon"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

func ensureCouponEnabled(couponInfo *coupon.Coupon) error {
	if !couponInfo.IsEnabled() {
		return errors.Wrapf(xerr.NewErrCode(xerr.CouponDisabled), "coupon disabled")
	}
	// Coupon start/expire times are stored as Unix milliseconds, matching the
	// admin editor. Comparing them against a seconds clock would put every
	// coupon that carries a start time permanently out of its window.
	now := time.Now().UnixMilli()
	if couponInfo.StartTime > 0 && now < couponInfo.StartTime {
		return errors.Wrapf(xerr.NewErrCode(xerr.CouponNotApplicable), "coupon is not active")
	}
	if couponInfo.ExpireTime <= 0 || now > couponInfo.ExpireTime {
		return errors.Wrapf(xerr.NewErrCode(xerr.CouponExpired), "coupon expired")
	}
	return nil
}

func calculateCoupon(amount int64, couponInfo *coupon.Coupon) int64 {
	if couponInfo.Type == 1 {
		return int64(float64(amount) * (float64(couponInfo.Discount) / float64(100)))
	} else {
		return min(couponInfo.Discount, amount)
	}
}
