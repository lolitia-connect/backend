// Package orderstream defines how committed order events are broadcast to
// already-connected clients.
//
// Redis Pub/Sub here is only a wake-up path: a message carries an event id and
// nothing else, and every consumer re-reads the durable row from the
// `order_event` outbox. That is what lets a subscriber that missed a message —
// or that connected after it was published — still reach the correct state.
package orderstream

const redisChannelPrefix = "order-events:"

// Channel returns the Redis Pub/Sub channel for one order's event stream.
func Channel(orderNo string) string {
	return redisChannelPrefix + orderNo
}
