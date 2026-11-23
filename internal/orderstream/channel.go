// Package orderstream defines how committed order events are broadcast to
// already-connected clients.
//
// Redis Pub/Sub here is only a wake-up path: a message carries an event id and
// nothing else, and every consumer re-reads the durable row from the
// `order_event` outbox. That is what lets a subscriber that missed a message —
// or that connected after it was published — still reach the correct state.
package orderstream

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Wake asks connected clients to read committed events. Periodic DB replay
// covers missed hints, so publishing must not hold up a payment response.
func Wake(ctx context.Context, client *redis.Client, orderNo string) {
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	if err := client.Publish(ctx, Channel(orderNo), "0").Err(); err != nil {
		zap.S().Debugw("Order notification deferred to replay", "order_no", orderNo, "error", err)
	}
}

const redisChannelPrefix = "order-events:"

// Channel returns the Redis Pub/Sub channel for one order's event stream.
func Channel(orderNo string) string {
	return redisChannelPrefix + orderNo
}
