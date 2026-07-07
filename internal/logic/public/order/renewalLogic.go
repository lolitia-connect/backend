package order

import (
	"context"
	"encoding/json"
	"github.com/perfect-panel/server/ent"
	"time"

	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/pkg/constant"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type RenewalLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewRenewalLogic creates a new renewal logic instance for subscription renewal operations
func NewRenewalLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RenewalLogic {
	return &RenewalLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Renewal processes subscription renewal orders including discount calculation,
// coupon validation, gift amount deduction, fee calculation, and order creation
func (l *RenewalLogic) Renewal(req *types.RenewalOrderRequest) (resp *types.RenewalOrderResponse, err error) {
	store := l.svcCtx.Store
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	if req.Quantity <= 0 {
		l.Logger.Debugf("[Renewal] Quantity is less than or equal to 0, setting to 1")
		req.Quantity = 1
	}

	// Validate quantity limit
	if req.Quantity > MaxQuantity {
		l.Logger.Errorw("[Renewal] Quantity exceeds maximum limit", zap.Any("quantity", req.Quantity), zap.Any("max", MaxQuantity))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "quantity exceeds maximum limit of %d", MaxQuantity)
	}

	orderNo := tool.GenerateTradeNo()
	// find user subscribe
	userSubscribe, err := store.User().FindOneUserSubscribe(l.ctx, req.UserSubscribeID)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user subscribe error: %v", err.Error())
	}
	// find subscription
	sub, err := store.Subscribe().FindOne(l.ctx, userSubscribe.SubscribeId)
	if err != nil {
		l.Logger.Errorw("[Renewal] Database query error", zap.Any("error", err.Error()), zap.Any("subscribe_id", userSubscribe.SubscribeId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe error: %v", err.Error())
	}
	// check subscribe plan status
	if !*sub.Sell {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "subscribe not sell")
	}
	var discount float64 = 1
	if sub.Discount != "" {
		var dis []types.SubscribeDiscount
		_ = json.Unmarshal([]byte(sub.Discount), &dis)
		discount = getDiscount(dis, req.Quantity)
	}
	price := sub.UnitPrice * req.Quantity
	amount := int64(float64(price) * discount)
	discountAmount := price - amount

	// Validate amount to prevent overflow
	if amount > MaxOrderAmount {
		l.Logger.Errorw("[Renewal] Order amount exceeds maximum limit",
			zap.Any("amount", amount),
			zap.Any("max", MaxOrderAmount),
			zap.Any("user_id", u.Id),
			zap.Any("subscribe_id", sub.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "order amount exceeds maximum limit")
	}

	var coupon int64 = 0
	if req.Coupon != "" {
		couponInfo, err := store.Coupon().FindOneByCode(l.ctx, req.Coupon)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponNotExist), "coupon not found")
			}
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find coupon error: %v", err.Error())
		}
		if err := ensureCouponEnabled(couponInfo); err != nil {
			return nil, err
		}
		if couponInfo.Count != 0 && couponInfo.Count <= couponInfo.UsedCount {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponInsufficientUsage), "coupon used")
		}
		couponSub := tool.StringToInt64Slice(couponInfo.Subscribe)

		if len(couponSub) > 0 && !tool.Contains(couponSub, sub.Id) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponNotApplicable), "coupon not match")
		}
		count, err := store.Order().CountUserCouponUsage(l.ctx, u.Id, req.Coupon)
		if err != nil {
			l.Logger.Errorw("[Renewal] Database query error", zap.Any("error", err.Error()), zap.Any("user_id", u.Id), zap.Any("coupon", req.Coupon))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find coupon error: %v", err.Error())
		}
		if count >= couponInfo.UserLimit {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponInsufficientUsage), "coupon limit exceeded")
		}
		coupon = calculateCoupon(amount, couponInfo)
	}
	payment, err := store.Payment().FindOne(l.ctx, req.Payment)
	if err != nil {
		l.Logger.Errorw("[Renewal] Database query error", zap.Any("error", err.Error()), zap.Any("payment", req.Payment))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find payment error: %v", err.Error())
	}
	amount -= coupon

	var deductionAmount int64
	// Check user deduction amount
	if u.GiftAmount > 0 {
		if u.GiftAmount >= amount {
			deductionAmount = amount
			u.GiftAmount -= deductionAmount
			amount = 0
		} else {
			deductionAmount = u.GiftAmount
			amount -= u.GiftAmount
			u.GiftAmount = 0
		}
	}

	var feeAmount int64
	// Calculate the handling fee
	if amount > 0 {
		feeAmount = calculateFee(amount, payment)
	}

	amount += feeAmount

	// Final validation after adding fee
	if amount > MaxOrderAmount {
		l.Logger.Errorw("[Renewal] Final order amount exceeds maximum limit after fee",
			zap.Any("amount", amount),
			zap.Any("max", MaxOrderAmount),
			zap.Any("user_id", u.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "order amount exceeds maximum limit")
	}

	// create order
	orderInfo := order.Order{
		UserId:         u.Id,
		ParentId:       userSubscribe.OrderId,
		OrderNo:        orderNo,
		Type:           2,
		Quantity:       req.Quantity,
		Price:          price,
		Amount:         amount,
		GiftAmount:     deductionAmount,
		Discount:       discountAmount,
		Coupon:         req.Coupon,
		CouponDiscount: coupon,
		PaymentId:      payment.Id,
		Method:         payment.Platform,
		FeeAmount:      feeAmount,
		Status:         1,
		SubscribeId:    userSubscribe.SubscribeId,
		SubscribeToken: userSubscribe.Token,
	}
	// Database transaction
	err = store.InTx(l.ctx, func(txStore repository.Store) error {
		// update user deduction && Pre deduction ,Return after canceling the order
		if orderInfo.GiftAmount > 0 {
			// update user deduction && Pre deduction ,Return after canceling the order
			if err := txStore.User().Update(l.ctx, u); err != nil {
				l.Logger.Errorw("[Renewal] Database update error", zap.Any("error", err.Error()), zap.Any("user", u))
				return err
			}
			// create deduction record
			giftLog := log.Gift{
				Type:        log.GiftTypeReduce,
				OrderNo:     orderInfo.OrderNo,
				SubscribeId: 0,
				Amount:      orderInfo.GiftAmount,
				Balance:     u.GiftAmount,
				Remark:      "Renewal order deduction",
				Timestamp:   time.Now().UnixMilli(),
			}
			content, _ := giftLog.Marshal()

			if err := txStore.Log().Insert(l.ctx, &log.SystemLog{
				Type:     log.TypeGift.Uint8(),
				Date:     time.Now().Format(time.DateOnly),
				ObjectID: u.Id,
				Content:  string(content),
			}); err != nil {
				l.Logger.Errorw("[Renewal] Database insert error", zap.Any("error", err.Error()), zap.Any("deductionLog", giftLog))
				return err
			}
		}
		// insert order
		return txStore.Order().Insert(l.ctx, &orderInfo)
	})
	if err != nil {
		l.Logger.Errorw("[Renewal] Database insert error", zap.Any("error", err.Error()), zap.Any("order", orderInfo))
		return nil, errors.Wrapf(err, "insert order error: %v", err.Error())
	}
	// Deferred task
	payload := queue.DeferCloseOrderPayload{
		OrderNo: orderInfo.OrderNo,
	}
	val, err := json.Marshal(payload)
	if err != nil {
		l.Logger.Errorw("[Renewal] Marshal payload error", zap.Any("error", err.Error()), zap.Any("payload", payload))
	}
	task := asynq.NewTask(queue.DeferCloseOrder, val, asynq.MaxRetry(3))
	taskInfo, err := l.svcCtx.Queue.Enqueue(task, asynq.ProcessIn(CloseOrderTimeMinutes*time.Minute))
	if err != nil {
		l.Logger.Errorw("[Renewal] Enqueue task error", zap.Any("error", err.Error()), zap.Any("task", task))
	} else {
		l.Logger.Infow("[Renewal] Enqueue task success", zap.Any("TaskID", taskInfo.ID))
	}
	return &types.RenewalOrderResponse{
		OrderNo: orderInfo.OrderNo,
	}, nil
}
