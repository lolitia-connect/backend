package orderflow

import (
	"context"

	"github.com/perfect-panel/server/internal/model/order"
)

// Idempotency carries V2-only creation metadata through the existing domain
// creators. V1 callers never attach it, so their persisted representation and
// behaviour remain unchanged.
type Idempotency struct {
	Key                string
	Hash               string
	GuestCheckoutToken string
}

type idempotencyContextKey struct{}

func WithIdempotency(ctx context.Context, value Idempotency) context.Context {
	return context.WithValue(ctx, idempotencyContextKey{}, value)
}

// ApplyIdempotency stamps the creation metadata carried by ctx onto an order
// that is about to be inserted. Creators call it unconditionally: on a V1
// request the context carries no value and the order is left untouched.
func ApplyIdempotency(ctx context.Context, data *order.Order) {
	value, ok := ctx.Value(idempotencyContextKey{}).(Idempotency)
	if !ok {
		return
	}
	data.IdempotencyKey = value.Key
	data.IdempotencyHash = value.Hash
}

// GuestCheckoutToken returns the guest checkout capability the caller asked to
// reuse. A guest creator uses it to make a retried creation idempotent without
// rotating the capability the caller already holds.
func GuestCheckoutToken(ctx context.Context) string {
	value, ok := ctx.Value(idempotencyContextKey{}).(Idempotency)
	if !ok {
		return ""
	}
	return value.GuestCheckoutToken
}
