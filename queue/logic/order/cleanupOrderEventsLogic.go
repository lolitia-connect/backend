package orderLogic

import (
	"context"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"github.com/perfect-panel/server/internal/svc"
)

// orderEventRetention bounds how far back a client may resume a stream with
// Last-Event-ID. An order's current state is always available from the order
// itself, so falling out of this window loses convenience, never correctness.
const orderEventRetention = 30 * 24 * time.Hour

// CleanupOrderEventsLogic removes only events that have already been published
// and are past the replay window. Unpublished events are never deleted, even if
// a Redis outage outlasts the retention period.
type CleanupOrderEventsLogic struct {
	svcCtx *svc.ServiceContext
}

func NewCleanupOrderEventsLogic(svcCtx *svc.ServiceContext) *CleanupOrderEventsLogic {
	return &CleanupOrderEventsLogic{svcCtx: svcCtx}
}

func (l *CleanupOrderEventsLogic) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	deleted, err := l.svcCtx.Store.OrderEvent().DeletePublishedBefore(ctx, time.Now().Add(-orderEventRetention))
	if err != nil {
		return err
	}
	if deleted > 0 {
		zap.S().Infof("removed %d expired order events", deleted)
	}
	return nil
}
