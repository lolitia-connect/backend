package orderLogic

import (
	"context"
	"encoding/json"
	"fmt"

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
		return fmt.Errorf("invalid close-order payload: %w: %v", asynq.SkipRetry, err)
	}
	if payload.OrderNo == "" {
		return fmt.Errorf("close-order number is empty: %w", asynq.SkipRetry)
	}
	return order.NewCloseOrderLogic(ctx, l.svc).ExpireOrder(&internal.CloseOrderRequest{
		OrderNo: payload.OrderNo,
	})
}
