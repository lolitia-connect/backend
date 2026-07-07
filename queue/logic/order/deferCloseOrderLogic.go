package orderLogic

import (
	"context"
	"encoding/json"

	"go.uber.org/zap"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/logic/public/order"
	"github.com/perfect-panel/server/internal/svc"
	internal "github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/queue/types"
)

type DeferCloseOrderLogic struct {
	svc *svc.ServiceContext
}

func NewDeferCloseOrderLogic(svc *svc.ServiceContext) *DeferCloseOrderLogic {
	return &DeferCloseOrderLogic{
		svc: svc,
	}
}

func (l *DeferCloseOrderLogic) ProcessTask(ctx context.Context, task *asynq.Task) error {
	payload := types.DeferCloseOrderPayload{}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		zap.S().Error("[DeferCloseOrderLogic] Unmarshal payload failed",
			zap.Any("error", err.Error()),
			zap.Any("payload", string(task.Payload())),
		)
		return nil
	}

	err := order.NewCloseOrderLogic(ctx, l.svc).CloseOrder(&internal.CloseOrderRequest{
		OrderNo: payload.OrderNo,
	})
	count, ok := asynq.GetRetryCount(ctx)
	if !ok {
		return nil
	}
	if err != nil && count < 3 {
		return err
	}
	return nil
}
