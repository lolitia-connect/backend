package orderflow

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
)

func ReserveCoupon(ctx context.Context, store repository.Store, current *order.Order) error {
	if current.Coupon == "" {
		return nil
	}
	coupon, err := store.Coupon().FindOneByCode(ctx, current.Coupon)
	if err != nil {
		return err
	}
	if !coupon.ReservableAt(time.Now()) {
		return xerr.NewErrCodeMsg(xerr.InvalidParams, "coupon is unavailable")
	}
	plans := tool.StringToInt64Slice(coupon.Subscribe)
	if len(plans) > 0 && !tool.Contains(plans, current.SubscribeId) {
		return xerr.NewErrCode(xerr.CouponNotApplicable)
	}
	if err := store.Coupon().ReserveUsage(ctx, current.Coupon); err != nil {
		return err
	}
	if coupon.UserLimit > 0 {
		var count int64
		if current.UserId > 0 {
			count, err = store.Order().CountUserCouponUsage(ctx, current.UserId, current.Coupon)
		} else if current.GuestAuthType != "" && current.GuestIdentifier != "" {
			count, err = store.Order().CountGuestCouponUsage(ctx, current.GuestAuthType, current.GuestIdentifier, current.Coupon)
		} else {
			return xerr.NewErrCodeMsg(xerr.CouponInsufficientUsage, "guest coupon usage cannot be verified")
		}
		if err != nil {
			return err
		}
		if count >= coupon.UserLimit {
			return xerr.NewErrCode(xerr.CouponInsufficientUsage)
		}
	}
	current.CouponReserved = true
	return nil
}
