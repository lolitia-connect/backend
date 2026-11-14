package orderLogic

import (
	"context"
	"strconv"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"github.com/perfect-panel/server/internal/orderstream"
	"github.com/perfect-panel/server/internal/svc"
)

// PublishOrderEventsLogic drains the durable order event outbox into Redis.
//
// Publishing is deliberately separate from writing an event: a Redis outage can
// delay a notification but can never roll back a committed payment state. An
// event that fails to publish stays unpublished and is retried, and the stream
// handler can also read it straight from the outbox.
type PublishOrderEventsLogic struct {
	svcCtx *svc.ServiceContext
}

func NewPublishOrderEventsLogic(svcCtx *svc.ServiceContext) *PublishOrderEventsLogic {
	return &PublishOrderEventsLogic{svcCtx: svcCtx}
}

func (l *PublishOrderEventsLogic) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	events, err := l.svcCtx.Store.OrderEvent().ListUnpublished(ctx, 500)
	if err != nil {
		return err
	}
	published := 0
	for _, event := range events {
		// The message is only a wake-up hint carrying the event id; subscribers
		// read the durable row themselves.
		if err := l.svcCtx.Redis.Publish(ctx, orderstream.Channel(event.OrderNo), strconv.FormatInt(event.Id, 10)).Err(); err != nil {
			// Stop at the first failure so the backlog is retried in order
			// instead of fanning out past the event that could not be sent.
			if published > 0 {
				zap.S().Warnf("published %d order events before a redis failure: %s", published, err.Error())
			}
			return err
		}
		if _, err := l.svcCtx.Store.OrderEvent().MarkPublished(ctx, event.Id, time.Now()); err != nil {
			return err
		}
		published++
	}
	if published > 0 {
		zap.S().Debugf("published %d order events", published)
	}
	return nil
}
