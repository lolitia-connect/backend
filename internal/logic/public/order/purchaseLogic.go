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
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type PurchaseLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

const (
	CloseOrderTimeMinutes = 15
)

// NewPurchaseLogic creates a new purchase logic instance for subscription purchase operations.
// It initializes the logger with context and sets up the service context for database operations.
func NewPurchaseLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PurchaseLogic {
	return &PurchaseLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Purchase processes new subscription purchase orders including validation, discount calculation,
// coupon processing, gift amount deduction, fee calculation, and order creation with database transaction.
// It handles the complete purchase workflow from user validation to order creation and task scheduling.
func (l *PurchaseLogic) Purchase(req *types.PurchaseOrderRequest) (resp *types.PurchaseOrderResponse, err error) {
	store := l.svcCtx.Store

	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	if req.Quantity <= 0 {
		l.Logger.Debugf("[Purchase] Quantity is less than or equal to 0, setting to 1")
		req.Quantity = 1
	}

	// Validate quantity limit
	if req.Quantity > MaxQuantity {
		l.Logger.Errorw("[Purchase] Quantity exceeds maximum limit", zap.Any("quantity", req.Quantity), zap.Any("max", MaxQuantity))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "quantity exceeds maximum limit of %d", MaxQuantity)
	}

	// find user subscription
	userSub, err := store.User().QueryUserSubscribe(l.ctx, u.Id)
	if err != nil {
		l.Logger.Errorw("[Purchase] Database query error", zap.Any("error", err.Error()), zap.Any("user_id", u.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user subscription error: %v", err.Error())
	}
	if l.svcCtx.Config.Subscribe.SingleModel {
		if len(userSub) > 0 {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserSubscribeExist), "user has subscription")
		}
	}

	// find subscribe plan
	sub, err := store.Subscribe().FindOne(l.ctx, req.SubscribeId)

	if err != nil {
		l.Logger.Errorw("[Purchase] Database query error", zap.Any("error", err.Error()), zap.Any("subscribe_id", req.SubscribeId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe error: %v", err.Error())
	}
	// check subscribe plan status
	if !*sub.Sell {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "subscribe not sell")
	}

	// check subscribe plan inventory
	if sub.Inventory == 0 {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeOutOfStock), "subscribe out of stock")
	}

	var discount float64 = 1
	if sub.Discount != "" {
		var dis []types.SubscribeDiscount
		_ = json.Unmarshal([]byte(sub.Discount), &dis)
		discount = getDiscount(dis, req.Quantity)
	}
	price := sub.UnitPrice * req.Quantity
	// discount amount
	amount := int64(float64(price) * discount)
	discountAmount := price - amount

	// Validate amount to prevent overflow
	if amount > MaxOrderAmount {
		l.Logger.Errorw("[Purchase] Order amount exceeds maximum limit",
			zap.Any("amount", amount),
			zap.Any("max", MaxOrderAmount),
			zap.Any("user_id", u.Id),
			zap.Any("subscribe_id", req.SubscribeId))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "order amount exceeds maximum limit")
	}

	var coupon int64 = 0
	// Calculate the coupon deduction
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
		if len(couponSub) > 0 && !tool.Contains(couponSub, req.SubscribeId) {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponNotApplicable), "coupon not match")
		}
		count, err := store.Order().CountUserCouponUsage(l.ctx, u.Id, req.Coupon)
		if err != nil {
			l.Logger.Errorw("[Purchase] Database query error", zap.Any("error", err.Error()), zap.Any("user_id", u.Id), zap.Any("coupon", req.Coupon))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find coupon error: %v", err.Error())
		}
		if couponInfo.UserLimit > 0 && count >= couponInfo.UserLimit {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.CouponInsufficientUsage), "coupon limit exceeded")
		}
		coupon = calculateCoupon(amount, couponInfo)
	}
	// Calculate the handling fee
	amount -= coupon
	// find payment method
	payment, err := store.Payment().FindOne(l.ctx, req.Payment)
	if err != nil {
		l.Logger.Errorw("[Purchase] Database query error", zap.Any("error", err.Error()), zap.Any("payment", req.Payment))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find payment method error: %v", err.Error())
	}
	var feeAmount int64
	// Calculate the handling fee
	if amount > 0 {
		feeAmount = calculateFee(amount, payment)
		amount += feeAmount

		// Final validation after adding fee
		if amount > MaxOrderAmount {
			l.Logger.Errorw("[Purchase] Final order amount exceeds maximum limit after fee",
				zap.Any("amount", amount),
				zap.Any("max", MaxOrderAmount),
				zap.Any("user_id", u.Id))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "order amount exceeds maximum limit")
		}
	}

	var deductionAmount int64
	// Gift amount is deducted after payment fee, because the fee is based on the payable cash amount.
	if u.GiftAmount > 0 && amount > 0 {
		if u.GiftAmount >= amount {
			deductionAmount = amount
			amount = 0
		} else {
			deductionAmount = u.GiftAmount
			amount -= u.GiftAmount
		}
	}

	// query user is new purchase or renewal
	isNew, err := store.Order().IsUserEligibleForNewOrder(l.ctx, u.Id)
	if err != nil {
		l.Logger.Errorw("[Purchase] Database query error", zap.Any("error", err.Error()), zap.Any("user_id", u.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user order error: %v", err.Error())
	}
	// create order
	orderInfo := &order.Order{
		UserId:         u.Id,
		OrderNo:        tool.GenerateTradeNo(),
		Type:           1,
		Quantity:       req.Quantity,
		Price:          price,
		Amount:         amount,
		Discount:       discountAmount,
		GiftAmount:     deductionAmount,
		Coupon:         req.Coupon,
		CouponDiscount: coupon,
		PaymentId:      payment.Id,
		Method:         payment.Platform,
		FeeAmount:      feeAmount,
		Status:         1,
		IsNew:          isNew,
		SubscribeId:    req.SubscribeId,
	}
	// Database transaction
	err = store.InTx(l.ctx, func(txStore repository.Store) error {
		if sub.Quota > 0 {
			count, e := txStore.User().CountUserSubscribesByUserAndSubscribe(l.ctx, u.Id, req.SubscribeId)
			if e != nil {
				l.Logger.Errorw("[Purchase] Database query error", zap.Any("error", e.Error()), zap.Any("user_id", u.Id), zap.Any("subscribe_id", req.SubscribeId))
				return e
			}
			if count >= sub.Quota {
				return errors.Wrapf(xerr.NewErrCode(xerr.SubscribeQuotaLimit), "quota limit")
			}
		}

		// update user deduction && Pre deduction ,Return after canceling the order
		if orderInfo.GiftAmount > 0 {
			// update user deduction && Pre deduction ,Return after canceling the order
			u.GiftAmount -= orderInfo.GiftAmount
			if e := txStore.User().Update(l.ctx, u); e != nil {
				l.Logger.Errorw("[Purchase] Database update error", zap.Any("error", e.Error()), zap.Any("user", u))
				return e
			}
			// create deduction record
			giftLog := log.Gift{
				Type:        log.GiftTypeReduce,
				OrderNo:     orderInfo.OrderNo,
				SubscribeId: 0,
				Amount:      orderInfo.GiftAmount,
				Balance:     u.GiftAmount,
				Remark:      "Purchase order deduction",
				Timestamp:   time.Now().UnixMilli(),
			}
			content, _ := giftLog.Marshal()

			if e := txStore.Log().Insert(l.ctx, &log.SystemLog{
				Type:     log.TypeGift.Uint8(),
				Date:     time.Now().Format(time.DateOnly),
				ObjectID: u.Id,
				Content:  string(content),
			}); e != nil {
				l.Logger.Errorw("[Purchase] Database insert error",
					zap.Any("error", e.Error()),
					zap.Any("deductionLog", giftLog),
				)
				return e
			}
		}

		if sub.Inventory != -1 {
			// decrease subscribe plan stock
			sub.Inventory -= 1
			// update subscribe plan stock
			if err = txStore.Subscribe().Update(l.ctx, sub); err != nil {
				l.Logger.Errorw("[Purchase] Database update error", zap.Any("error", err.Error()), zap.Any("subscribe", sub))
				return err
			}
		}

		// insert order
		return txStore.Order().Insert(l.ctx, orderInfo)
	})
	if err != nil {
		l.Logger.Errorw("[Purchase] Database insert error", zap.Any("error", err.Error()), zap.Any("orderInfo", orderInfo))
		var codeErr *xerr.CodeError
		if errors.As(err, &codeErr) {
			return nil, err
		}
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert order error: %v", err.Error())
	}
	// Deferred task
	payload := queue.DeferCloseOrderPayload{
		OrderNo: orderInfo.OrderNo,
	}
	val, err := json.Marshal(payload)
	if err != nil {
		l.Logger.Errorw("[Purchase] Marshal payload error", zap.Any("error", err.Error()), zap.Any("payload", payload))
	}
	task := asynq.NewTask(queue.DeferCloseOrder, val, asynq.MaxRetry(3))
	taskInfo, err := l.svcCtx.Queue.Enqueue(task, asynq.ProcessIn(CloseOrderTimeMinutes*time.Minute))
	if err != nil {
		l.Logger.Errorw("[Purchase] Enqueue task error", zap.Any("error", err.Error()), zap.Any("task", task))
	} else {
		l.Logger.Infow("[Purchase] Enqueue task success", zap.Any("TaskID", taskInfo.ID))
	}

	return &types.PurchaseOrderResponse{
		OrderNo: orderInfo.OrderNo,
	}, nil
}
