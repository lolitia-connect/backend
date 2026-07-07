package order

import (
	"context"
	"encoding/json"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	queue "github.com/perfect-panel/server/queue/types"
	"go.uber.org/zap"
)

type UpdateOrderStatusLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update order status
func NewUpdateOrderStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateOrderStatusLogic {
	return &UpdateOrderStatusLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateOrderStatusLogic) UpdateOrderStatus(req *types.UpdateOrderStatusRequest) error {
	store := l.svcCtx.Store
	info, err := store.Order().FindOne(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorw("[UpdateOrderStatus] FindOne error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOne error: %v", err.Error())
	}

	if req.PaymentId != 0 {
		paymentMethod, err := store.Payment().FindOne(l.ctx, req.PaymentId)
		if err != nil {
			l.Logger.Error("[CreateOrder] PaymentMethod Not Found", zap.Any("error", err.Error()))
			return errors.Wrapf(xerr.NewErrCode(xerr.PaymentMethodNotFound), "PaymentMethod not found: %v", err.Error())
		}
		info.PaymentId = paymentMethod.Id
		info.Method = paymentMethod.Platform
	}
	if req.TradeNo != "" {
		info.TradeNo = req.TradeNo
	}

	err = store.InTx(l.ctx, func(txStore repository.Store) error {
		orderStore := txStore.Order()
		if err := orderStore.Update(l.ctx, info); err != nil {
			l.Logger.Errorw("[UpdateOrderStatus] Update error", zap.Any("error", err.Error()), zap.Any("OrderID", info.Id))
			return err
		}
		if err := orderStore.UpdateOrderStatus(l.ctx, info.OrderNo, req.Status); err != nil {
			return err
		}
		// If order status is 2, create user subscription
		if req.Status == 2 {
			payload := queue.ForthwithActivateOrderPayload{
				OrderNo: info.OrderNo,
			}
			p, _ := json.Marshal(payload)
			task := asynq.NewTask(queue.ForthwithActivateOrder, p)
			_, err = l.svcCtx.Queue.EnqueueContext(l.ctx, task)
			if err != nil {
				l.Logger.Errorw("[UpdateOrderStatus] Enqueue error", zap.Any("error", err.Error()))
				return errors.Wrapf(xerr.NewErrCode(xerr.QueueEnqueueError), "Enqueue error: %v", err.Error())
			}
		}
		return nil
	})
	if err != nil {
		l.Logger.Errorw("[UpdateOrderStatus] Transaction error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Transaction error: %v", err.Error())
	}
	return nil
}
