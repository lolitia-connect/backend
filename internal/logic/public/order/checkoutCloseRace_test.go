package order

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/internal/logic/public/portal"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tasklock"
	"github.com/redis/go-redis/v9"
)

// An audit claimed the close was not fenced against a checkout starting
// mid-close: the order is read unlocked in `CloseOrder`
// (closeOrderLogic.go:69), `closeGateway` runs outside the transaction
// (closeOrderLogic.go:95), and `ClosePendingOrder` (transition.go:78) only
// requires `Status == Pending`. A checkout committing `SetPaymentExpectation`
// (payment_snapshot.go:64) in that window would leave a payable gateway session
// on a closed order.
//
// That interleaving is impossible: `CloseOrder` takes the
// `order-checkout:<orderNo>` task lock at closeOrderLogic.go:54 for its whole
// duration, and `PurchaseCheckout` takes the *same* key at
// purchaseCheckoutLogic.go:58 for its whole duration. The two are serialised, so
// the unlocked read and the gateway verdict cannot be overtaken by a checkout.
// These tests pin that fence from both orderings. Deleting either
// `tasklock.Acquire` makes them fail.

const checkoutLockOrderNo = "race-order"

// holdOrderCheckoutLock takes the mutex a checkout holds for its whole run, so
// the test can observe the other side's refusal. Released by t.Cleanup.
func holdOrderCheckoutLock(t *testing.T, client *redis.Client) {
	t.Helper()
	lock, held, err := tasklock.Acquire(context.Background(), client, "order-checkout:"+checkoutLockOrderNo, 2*time.Minute)
	if err != nil || !held {
		t.Fatalf("pre-acquire order checkout lock: held=%t err=%v", held, err)
	}
	t.Cleanup(func() { _, _ = lock.Release(context.Background()) })
}

// payableState reports whether anything a gateway could still collect against
// is recorded on the order: an expectation, a claimed trade number, or a
// stored checkout session.
func payableState(current order.Order) bool {
	return current.PaymentCurrency != "" || current.PaymentSession != "" || current.TradeNo != ""
}

// Ordering 1: a checkout already committed an expectation, so the close must
// not take its "checkout never started" verdict (payment_close.go:28-30 fires
// only when both the trade number and the currency are empty).
func TestCloseDoesNotTreatACommittedExpectationAsNeverStarted(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	store := &closeStore{orders: closeOrders{current: order.Order{
		OrderNo: checkoutLockOrderNo, Status: order.StatusPending, Method: "Stripe", PaymentId: 7,
		// Recorded by the checkout before it creates the payable session.
		PaymentAmount: 1000, PaymentCurrency: "USD",
	}}}
	service := &svc.ServiceContext{Store: store, Redis: client}

	err := NewCloseOrderLogic(context.Background(), service).CloseOrder(&types.CloseOrderRequest{OrderNo: checkoutLockOrderNo})
	if err == nil {
		t.Fatal("close accepted an order whose checkout was already committed")
	}
	// The refusal comes from the gateway, not from the "never started" shortcut.
	if !strings.Contains(err.Error(), "has not been confirmed") {
		t.Fatalf("close error = %v, want the gateway verdict for a started checkout", err)
	}
	if store.orders.current.Status == order.StatusClosed {
		t.Fatal("order ended Closed while a payable session existed")
	}
}

// Ordering 2: the close commits first; the checkout that runs afterwards must
// be refused and must write no payment state onto the closed order.
func TestCheckoutAfterCloseWritesNoPayableState(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	store := &closeStore{orders: closeOrders{current: order.Order{
		OrderNo: checkoutLockOrderNo, Status: order.StatusPending, Method: "balance", UserId: 7,
	}}}
	service := &svc.ServiceContext{Store: store, Redis: client}

	if err := NewCloseOrderLogic(context.Background(), service).CloseOrder(&types.CloseOrderRequest{OrderNo: checkoutLockOrderNo}); err != nil {
		t.Fatalf("close: %v", err)
	}
	if store.orders.current.Status != order.StatusClosed {
		t.Fatalf("close left status %d, want %d", store.orders.current.Status, order.StatusClosed)
	}

	_, err := portal.NewPurchaseCheckoutLogic(context.Background(), service).PurchaseCheckout(&types.CheckoutOrderRequest{OrderNo: checkoutLockOrderNo})
	if err == nil {
		t.Fatal("checkout accepted an order that was already closed")
	}
	if payableState(store.orders.current) {
		t.Fatalf("checkout wrote payment state onto a closed order: %+v", store.orders.current)
	}
}

// The audited interleaving: with the close in flight, a checkout must not be
// able to start, so it can never commit an expectation between the close's
// unlocked read and its transactional write.
func TestCheckoutCannotStartWhileCloseIsInFlight(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	store := &closeStore{orders: closeOrders{current: order.Order{
		OrderNo: checkoutLockOrderNo, Status: order.StatusPending, Method: "Stripe", PaymentId: 7,
	}}}
	service := &svc.ServiceContext{Store: store, Redis: client}

	holdOrderCheckoutLock(t, client)

	_, err := portal.NewPurchaseCheckoutLogic(context.Background(), service).PurchaseCheckout(&types.CheckoutOrderRequest{OrderNo: checkoutLockOrderNo})
	if err == nil {
		t.Fatal("checkout started while the close held the order lock")
	}
	if !strings.Contains(err.Error(), "already being processed") {
		t.Fatalf("checkout error = %v, want the in-progress refusal", err)
	}
	if payableState(store.orders.current) {
		t.Fatalf("refused checkout still wrote payment state: %+v", store.orders.current)
	}
}

// The mirror: a close must not start while the checkout that recorded the
// expectation is still in flight, so it cannot base its gateway verdict on a
// pre-checkout snapshot.
func TestCloseCannotStartWhileCheckoutIsInFlight(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	store := &closeStore{orders: closeOrders{current: order.Order{
		OrderNo: checkoutLockOrderNo, Status: order.StatusPending, Method: "Stripe", PaymentId: 7,
	}}}
	service := &svc.ServiceContext{Store: store, Redis: client}

	holdOrderCheckoutLock(t, client)

	err := NewCloseOrderLogic(context.Background(), service).CloseOrder(&types.CloseOrderRequest{OrderNo: checkoutLockOrderNo})
	if err == nil {
		t.Fatal("close ran while a checkout held the order lock")
	}
	if !strings.Contains(err.Error(), "order checkout is in progress") {
		t.Fatalf("close error = %v, want the in-progress refusal", err)
	}
	if store.orders.current.Status != order.StatusPending {
		t.Fatalf("refused close changed status to %d", store.orders.current.Status)
	}
}
