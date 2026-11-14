package order

import (
	"context"
	"encoding/json"
	"time"

	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/orderaudit"
	"github.com/perfect-panel/server/internal/orderflow"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type ResetTrafficLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Reset traffic
func NewResetTrafficLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetTrafficLogic {
	return &ResetTrafficLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResetTrafficLogic) ResetTraffic(req *types.ResetTrafficOrderRequest) (resp *types.ResetTrafficOrderResponse, err error) {
	store := l.svcCtx.Store
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	// find user subscription
	userSubscribe, err := store.User().FindOneUserSubscribe(l.ctx, req.UserSubscribeID)
	if err != nil {
		l.Logger.Errorw("[ResetTraffic] Database query error", zap.Any("error", err.Error()), zap.Any("UserSubscribeID", req.UserSubscribeID))
		return nil, errors.Wrapf(err, "find user subscribe error: %v", err.Error())
	}
	if userSubscribe.Subscribe == nil {
		l.Logger.Errorw("[ResetTraffic] subscribe not found", zap.Any("UserSubscribeID", req.UserSubscribeID))
		return nil, errors.New("subscribe not found")
	}
	// Check if traffic is unlimited — reset is meaningless
	if userSubscribe.TrafficUnlimited {
		return nil, errors.New("traffic reset is not available for unlimited traffic subscriptions")
	}
	amount := userSubscribe.Subscribe.Replacement
	walletInfo, err := store.Wallet().FindOne(l.ctx, u.Id)
	if err != nil {
		l.Logger.Errorw("[ResetTraffic] Database query error", zap.Any("error", err.Error()), zap.Any("user_id", u.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find wallet error: %v", err.Error())
	}
	var deductionAmount int64
	// Check wallet deduction amount
	if walletInfo.GiftAmount > 0 {
		deductionAmount, amount = walletInfo.Reserve(amount)
	}
	// find payment method
	payment, err := store.Payment().FindOne(l.ctx, req.Payment)
	if err != nil {
		l.Logger.Errorw("[ResetTraffic] Database query error", zap.Any("error", err.Error()), zap.Any("payment", req.Payment))
		return nil, errors.Wrapf(err, "find payment error: %v", err.Error())
	}
	var feeAmount int64
	// Calculate the handling fee
	if amount > 0 {
		feeAmount = calculateFee(amount, payment)
	}
	// create order
	orderInfo := order.Order{
		Id:             0,
		ParentId:       userSubscribe.OrderId,
		UserId:         u.Id,
		OrderNo:        tool.GenerateTradeNo(),
		Type:           3,
		Price:          userSubscribe.Subscribe.Replacement,
		Amount:         amount + feeAmount,
		GiftAmount:     deductionAmount,
		FeeAmount:      feeAmount,
		PaymentId:      payment.Id,
		Method:         payment.Platform,
		Status:         1,
		SubscribeId:    userSubscribe.SubscribeId,
		SubscribeToken: userSubscribe.Token,
	}
	orderflow.ApplyIdempotency(l.ctx, &orderInfo)
	// Database transaction
	err = store.InTx(l.ctx, func(txStore repository.Store) error {
		// Reserve gift credit from a wallet read inside the transaction so a
		// concurrent order cannot spend the same balance.
		if orderInfo.GiftAmount > 0 {
			fresh, err := txStore.Wallet().FindOne(l.ctx, u.Id)
			if err != nil {
				return err
			}
			if fresh.GiftAmount < orderInfo.GiftAmount {
				return errors.Wrapf(xerr.NewErrCode(xerr.InsufficientBalance), "insufficient gift balance")
			}
			fresh.GiftAmount -= orderInfo.GiftAmount
			if err := txStore.Wallet().UpdateBalanceFields(l.ctx, fresh); err != nil {
				l.Logger.Errorw("[ResetTraffic] Database update error", zap.Any("error", err.Error()), zap.Any("wallet", fresh))
				return err
			}
			// create deduction record
			giftLog := log.Gift{
				Type:        log.GiftTypeReduce,
				OrderNo:     orderInfo.OrderNo,
				SubscribeId: 0,
				Amount:      orderInfo.GiftAmount,
				Balance:     fresh.GiftAmount,
				Remark:      "Renewal order deduction",
				Timestamp:   time.Now().UnixMilli(),
			}
			content, _ := giftLog.Marshal()

			if err = txStore.Log().Insert(l.ctx, &log.SystemLog{
				Type:     log.TypeGift.Uint8(),
				Date:     time.Now().Format(time.DateOnly),
				ObjectID: u.Id,
				Content:  string(content),
			}); err != nil {
				l.Logger.Errorw("[ResetTraffic] Database insert error", zap.Any("error", err.Error()), zap.Any("deductionLog", content))
				return err
			}
		}
		// insert order
		if err = txStore.Order().Insert(l.ctx, &orderInfo); err != nil {
			return err
		}
		return orderaudit.InsertCreated(l.ctx, txStore.Log(), &orderInfo, orderaudit.SourceUser)
	})
	if err != nil {
		l.Logger.Errorw("[ResetTraffic] Database insert error", zap.Any("error", err.Error()), zap.Any("order", orderInfo))
		return nil, errors.Wrapf(err, "insert order error: %v", err.Error())
	}
	// Deferred task
	payload := queue.DeferCloseOrderPayload{
		OrderNo: orderInfo.OrderNo,
	}
	val, err := json.Marshal(payload)
	if err != nil {
		l.Logger.Errorw("[ResetTraffic] Marshal payload error", zap.Any("error", err.Error()), zap.Any("payload", payload))
	}
	task := asynq.NewTask(queue.DeferCloseOrder, val, asynq.MaxRetry(3))
	taskInfo, err := l.svcCtx.Queue.Enqueue(task, asynq.ProcessIn(CloseOrderTimeMinutes*time.Minute))
	if err != nil {
		l.Logger.Errorw("[ResetTraffic] Enqueue task error", zap.Any("error", err.Error()), zap.Any("task", task))
	} else {
		l.Logger.Infow("[ResetTraffic] Enqueue task success", zap.Any("TaskID", taskInfo.ID))
	}
	return &types.ResetTrafficOrderResponse{
		OrderNo: orderInfo.OrderNo,
	}, nil
}
