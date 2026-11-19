package user

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/deduction"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

func CalculateRemainingAmount(ctx context.Context, svcCtx *svc.ServiceContext, userSubscribeId int64) (int64, error) {
	// Find User Subscribe
	userSubscribe, err := svcCtx.Store.User().FindOneUserSubscribe(ctx, userSubscribeId)
	if err != nil {
		zap.S().Error("[func CalculateRemainingAmount(ctx context.Context, svcCtx *svc.ServiceContext, userSubscribeId int64) (int64, error) {\n] FindOneUserSubscribe", zap.Any("err", err.Error()), zap.Any("id", userSubscribeId))
		return 0, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneUserSubscribe failed, id: %d", userSubscribeId)
	}
	actor, ok := ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok || actor == nil || userSubscribe == nil || userSubscribe.UserId != actor.Id {
		return 0, xerr.NewErrCode(xerr.InvalidAccess)
	}
	if userSubscribe.EntitlementSource != "" {
		return 0, user.ErrProviderManaged
	}
	if userSubscribe.Status > 2 {
		return 0, errors.New("subscription cannot be refunded")
	}
	if userSubscribe.OrderId == 0 {
		return 0, nil
	}
	if userSubscribe.Subscribe == nil {
		return 0, xerr.NewErrCode(xerr.SubscribeNotAvailable)
	}
	if (userSubscribe.Subscribe.AllowDeduction == nil || !*userSubscribe.Subscribe.AllowDeduction) && !svcCtx.Config.Subscribe.SingleModel {
		return 0, errors.New("The subscription package does not support deductions")
	}
	if userSubscribe.Status != 1 {
		return 0, errors.New("the subscription package is not in use")
	}

	// Find Order Details
	orderDetails, err := svcCtx.Store.Order().FindOneDetails(ctx, userSubscribe.OrderId)
	if err != nil {
		zap.S().Error("[PreUnsubscribe] FindOneDetails", zap.Any("err", err.Error()), zap.Any("id", userSubscribe.OrderId))
		return 0, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneDetails failed, id: %d", userSubscribe.OrderId)
	}
	if orderDetails == nil || orderDetails.UserId != actor.Id {
		return 0, xerr.NewErrCode(xerr.InvalidAccess)
	}
	orderQuantity := orderDetails.Quantity
	orderAmount := orderDetails.RefundBasis()

	if len(orderDetails.SubOrders) > 0 {
		for _, subOrder := range orderDetails.SubOrders {
			if subOrder.IsPaidRenewal() {
				orderQuantity += subOrder.Quantity
			}
		}
	}
	// Calculate Remaining Amount
	remainingAmount, err := deduction.CalculateRemainingAmount(
		deduction.Subscribe{
			StartTime:        userSubscribe.StartTime,
			ExpireTime:       userSubscribe.ExpireTime,
			Traffic:          userSubscribe.Traffic,
			TrafficUnlimited: userSubscribe.TrafficUnlimited,
			Download:         userSubscribe.Download,
			Upload:           userSubscribe.Upload,
			UnitTime:         userSubscribe.Subscribe.UnitTime,
			UnitPrice:        userSubscribe.Subscribe.UnitPrice,
			ResetCycle:       userSubscribe.Subscribe.ResetCycle,
			DeductionRatio:   userSubscribe.Subscribe.DeductionRatio,
		},
		deduction.Order{
			Amount:   orderAmount,
			Quantity: orderQuantity,
		},
		time.Now(),
	)
	if err != nil {
		return 0, errors.Wrapf(xerr.NewErrCode(500), "CalculateRemainingAmount failed, userSubscribeId: %d, err: %v", userSubscribeId, err)
	}
	return min(max(remainingAmount, 0), orderAmount), nil
}
