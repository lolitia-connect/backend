package orderLogic

import (
	"context"
	"errors"
	"time"

	"github.com/hibiken/asynq"
	entorder "github.com/perfect-panel/server/ent/order"
	"github.com/perfect-panel/server/internal/logic/notify"
	publicorder "github.com/perfect-panel/server/internal/logic/public/order"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/payment"
	"github.com/redis/go-redis/v9"
)

// RecoverStripeOrders repairs work that was never enqueued or exhausted its retries.
// The cursor is only a scan position; losing Redis starts a fresh pass over the DB.
func RecoverStripeOrders(service *svc.ServiceContext) asynq.HandlerFunc {
	return func(ctx context.Context, _ *asynq.Task) error {
		const cursorKey = "order:stripe-recovery:cursor"
		const pageSize = 100
		after, err := service.Redis.Get(ctx, cursorKey).Int64()
		if err != nil && !errors.Is(err, redis.Nil) {
			return err
		}
		now := time.Now()
		rows, err := service.Ent.Order.Query().Where(
			entorder.Method(payment.Stripe.String()), entorder.IDGT(after),
			entorder.Or(
				entorder.And(entorder.Status(order.StatusPaid), entorder.UpdatedAtLT(now.Add(-time.Minute))),
				entorder.And(entorder.Status(order.StatusPending), entorder.CreatedAtLT(now.Add(-publicorder.CloseOrderTimeMinutes*time.Minute))),
			),
		).Order(entorder.ByID()).Limit(pageSize).Select(entorder.FieldID, entorder.FieldOrderNo, entorder.FieldStatus).All(ctx)
		if err != nil {
			return err
		}
		var enqueueErr error
		for _, row := range rows {
			if row.Status == order.StatusPaid {
				err = notify.EnqueuePaidOrder(ctx, service, row.OrderNo)
			} else {
				// Expiry checks Stripe before closing and settles a recovered payment.
				err = notify.EnqueueStripeCloseCheck(ctx, service, row.OrderNo)
			}
			if err != nil {
				enqueueErr = err
			}
		}
		var cursor int64
		if len(rows) == pageSize {
			cursor = rows[len(rows)-1].ID
		}
		return errors.Join(enqueueErr, service.Redis.Set(ctx, cursorKey, cursor, 0).Err())
	}
}
