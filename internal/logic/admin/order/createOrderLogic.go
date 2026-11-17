package order

import (
	"context"

	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/orderaudit"
	"github.com/perfect-panel/server/internal/orderflow"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type CreateOrderLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create order
func NewCreateOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateOrderLogic {
	return &CreateOrderLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateOrderLogic) CreateOrder(req *types.CreateOrderRequest) error {
	if err := validateCreateOrder(req); err != nil {
		return err
	}
	store := l.svcCtx.Store
	paymentMethod, err := store.Payment().FindOne(l.ctx, req.PaymentId)
	if err != nil {
		l.Logger.Error("[CreateOrder] PaymentMethod Not Found", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.PaymentMethodNotFound), "PaymentMethod not found: %v", err.Error())
	}

	orderInfo := &order.Order{
		UserId:         req.UserId,
		OrderNo:        tool.GenerateTradeNo(),
		Type:           req.Type,
		Quantity:       max(req.Quantity, 1),
		Price:          req.Price,
		Amount:         req.Amount,
		Discount:       req.Discount,
		Coupon:         req.Coupon,
		CouponDiscount: req.CouponDiscount,
		PaymentId:      req.PaymentId,
		Method:         paymentMethod.Platform,
		FeeAmount:      req.FeeAmount,
		Status:         order.StatusPending,
		SubscribeId:    req.SubscribeId,
	}
	// The order and its audit trail are one atomic billing operation.
	err = store.InTx(l.ctx, func(txStore repository.Store) error {
		owner, err := txStore.User().FindOne(l.ctx, orderInfo.UserId)
		if err != nil {
			return err
		}
		if owner == nil || owner.DeletedAt != nil {
			return xerr.NewErrCode(xerr.UserNotExist)
		}
		if _, err := txStore.Wallet().FindOneForUpdate(l.ctx, orderInfo.UserId); err != nil {
			return err
		}
		if req.Type == order.TypeRenewal || req.Type == order.TypeResetTraffic {
			target, err := txStore.User().FindOneSubscribeForUpdate(l.ctx, req.UserSubscribeId)
			if err != nil {
				return err
			}
			if target == nil || target.UserId != req.UserId || target.Status >= 4 || (req.SubscribeId != 0 && req.SubscribeId != target.SubscribeId) {
				return xerr.NewErrCodeMsg(xerr.InvalidParams, "subscription does not belong to this order")
			}
			if target.EntitlementSource != "" {
				return user.ErrProviderManaged
			}
			orderInfo.ParentId, orderInfo.UserSubscribeId = target.OrderId, target.Id
			orderInfo.SubscribeId, orderInfo.SubscribeToken = target.SubscribeId, target.Token
		}
		if orderInfo.Type != order.TypeRecharge {
			plan, err := txStore.Subscribe().FindOne(l.ctx, orderInfo.SubscribeId)
			if err != nil {
				return err
			}
			if plan == nil {
				return xerr.NewErrCodeMsg(xerr.InvalidParams, "subscription plan does not exist")
			}
		}
		if err := orderflow.ReserveCoupon(l.ctx, txStore, orderInfo); err != nil {
			return err
		}
		if orderInfo.Type == order.TypeSubscribe {
			if err := txStore.Subscribe().ReserveInventory(l.ctx, orderInfo.SubscribeId); err != nil {
				return err
			}
		}
		if e := txStore.Order().Insert(l.ctx, orderInfo); e != nil {
			return e
		}
		return orderaudit.InsertCreated(l.ctx, txStore.Log(), orderInfo, orderaudit.SourceAdmin)
	})
	if err != nil {
		l.Logger.Error("[CreateOrder] Database Error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "Insert error: %v", err.Error())
	}
	return nil
}

func validateCreateOrder(req *types.CreateOrderRequest) error {
	invalid := func(message string) error { return xerr.NewErrCodeMsg(xerr.InvalidParams, message) }
	if req == nil || req.UserId <= 0 || req.PaymentId == 0 {
		return invalid("user_id and payment_id are required")
	}
	if req.Status != 0 && req.Status != order.StatusPending {
		return invalid("admin-created orders must start pending")
	}
	if req.Quantity < 0 || req.Quantity > 1000 {
		return invalid("quantity must be between 1 and 1000, or omitted")
	}
	if req.TradeNo != "" {
		return invalid("trade_no is assigned by the payment gateway")
	}
	for _, amount := range []int64{req.Price, req.Amount, req.Discount, req.CouponDiscount, req.Commission, req.FeeAmount} {
		if amount < 0 || amount > 2147483647 {
			return invalid("order amounts must be non-negative and within supported limits")
		}
	}
	switch req.Type {
	case order.TypeSubscribe:
		if req.SubscribeId <= 0 || req.UserSubscribeId != 0 {
			return invalid("a subscription order requires a plan")
		}
	case order.TypeRenewal, order.TypeResetTraffic:
		if req.UserSubscribeId <= 0 {
			return invalid("a renewal or traffic reset requires user_subscribe_id")
		}
	case order.TypeRecharge:
		if req.SubscribeId != 0 || req.UserSubscribeId != 0 {
			return invalid("a recharge cannot target a subscription")
		}
	default:
		return invalid("unsupported order type")
	}
	if req.Coupon != "" && req.Type != order.TypeSubscribe && req.Type != order.TypeRenewal {
		return invalid("coupons apply to subscription and renewal orders only")
	}
	return nil
}
