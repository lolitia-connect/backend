package order

import (
	"testing"

	"github.com/perfect-panel/server/ent/orderevent"
)

func TestOrderPaymentAndFulfillmentTransitionsAreIdempotent(t *testing.T) {
	ctx := t.Context()
	client := newEventTestClient(t)
	model := NewModel(client, nil)
	client.Order.Create().SetOrderNo("transition-1").SetStatus(StatusPending).SaveX(ctx)
	client.Order.Create().SetOrderNo("transition-2").SetStatus(StatusPending).SaveX(ctx)

	if ok, err := model.MarkOrderPaid(ctx, "transition-1", ""); err == nil || ok {
		t.Fatalf("empty trade number accepted: %t, %v", ok, err)
	}
	if ok, err := model.MarkOrderPaid(ctx, "transition-1", "trade-1"); err != nil || !ok {
		t.Fatalf("first payment transition: %t, %v", ok, err)
	}
	if ok, err := model.MarkOrderPaid(ctx, "transition-1", "trade-1"); err != nil || ok {
		t.Fatalf("duplicate payment transition: %t, %v", ok, err)
	}
	current, err := model.FindOneByOrderNo(ctx, "transition-1")
	if err != nil || current.Status != StatusPaid || current.StateVersion != 1 || current.TradeNo != "trade-1" {
		t.Fatalf("paid order = %+v, %v", current, err)
	}
	events := client.OrderEvent.Query().AllX(ctx)
	if len(events) != 1 || events[0].EventType != EventOrderPaymentPaid {
		t.Fatalf("payment events = %+v", events)
	}
	if ok, err := model.FinishOrder(ctx, current.OrderNo); err != nil || !ok {
		t.Fatalf("finish transition: %t, %v", ok, err)
	}
	if ok, err := model.FinishOrder(ctx, current.OrderNo); err != nil || ok {
		t.Fatalf("duplicate finish transition: %t, %v", ok, err)
	}
	events = client.OrderEvent.Query().Order(orderevent.ByID()).AllX(ctx)
	if len(events) != 2 || events[1].EventType != EventOrderFulfilled {
		t.Fatalf("events after finish = %+v", events)
	}
	payload := ParseEventPayload(events[1].Payload)
	if payload.OrderNo != current.OrderNo || payload.StateVersion != 2 || payload.PaymentStatus != "paid" || payload.FulfillmentStatus != "finished" {
		t.Fatalf("unexpected fulfillment payload: %+v", payload)
	}

	if ok, err := model.SetPaymentExpectation(ctx, "transition-2", 1200, " cny "); err != nil || !ok {
		t.Fatalf("payment expectation: %t, %v", ok, err)
	}
	if ok, err := model.SetPaymentExpectation(ctx, "transition-2", 1500, "USD"); err != nil || ok {
		t.Fatalf("payment expectation was overwritten: %t, %v", ok, err)
	}
	if ok, err := model.BeginPaymentSession(ctx, "transition-2", ""); err == nil || ok {
		t.Fatalf("empty payment session accepted: %t, %v", ok, err)
	}
	if ok, err := model.BeginPaymentSession(ctx, "transition-2", "creating"); err != nil || !ok {
		t.Fatalf("begin payment session: %t, %v", ok, err)
	}
	if ok, err := model.CompletePaymentSession(ctx, "transition-2", "wrong", "trade-2", "done"); err != nil || ok {
		t.Fatalf("wrong session completed: %t, %v", ok, err)
	}
	if ok, err := model.CompletePaymentSession(ctx, "transition-2", "creating", "trade-2", "done"); err != nil || !ok {
		t.Fatalf("complete payment session: %t, %v", ok, err)
	}
	if ok, err := model.ClaimPaymentSession(ctx, "transition-2", "trade-2", "done"); err != nil || !ok {
		t.Fatalf("same trade claim: %t, %v", ok, err)
	}
	second, err := model.FindOneByOrderNo(ctx, "transition-2")
	if err != nil || second.PaymentAmount != 1200 || second.PaymentCurrency != "CNY" || second.PaymentSession != "done" || second.TradeNo != "trade-2" {
		t.Fatalf("second order payment state = %+v, %v", second, err)
	}
}
