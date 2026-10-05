package order

import (
	"encoding/json"
	"testing"
)

func TestEventTypeForStatus(t *testing.T) {
	cases := []struct {
		status uint8
		want   string
	}{
		{StatusPending, EventOrderStateChange},
		{StatusPaid, EventOrderPaymentPaid},
		{StatusClosed, EventOrderClosed},
		{StatusFailed, EventOrderStateChange},
		{StatusFinished, EventOrderFulfilled},
		{0, EventOrderStateChange},
		{99, EventOrderStateChange},
	}
	for _, tc := range cases {
		if got := EventTypeForStatus(tc.status); got != tc.want {
			t.Errorf("EventTypeForStatus(%d) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestPaymentAndFulfillmentStatusSeparateTheTwoLegs(t *testing.T) {
	cases := []struct {
		status            uint8
		payment           string
		fulfillmentStatus string
	}{
		// A pending order has neither money nor fulfilment.
		{StatusPending, "pending", "not_started"},
		// Paid is the interesting case: money is in, the subscription is not yet
		// usable, and a client must be able to tell that apart from both ends.
		{StatusPaid, "paid", "pending"},
		{StatusFinished, "paid", "finished"},
		{StatusClosed, "closed", "not_started"},
		{StatusFailed, "failed", "not_started"},
	}
	for _, tc := range cases {
		if got := PaymentStatus(tc.status); got != tc.payment {
			t.Errorf("PaymentStatus(%d) = %q, want %q", tc.status, got, tc.payment)
		}
		if got := FulfillmentStatus(tc.status); got != tc.fulfillmentStatus {
			t.Errorf("FulfillmentStatus(%d) = %q, want %q", tc.status, got, tc.fulfillmentStatus)
		}
	}
}

func TestNewEventForCarriesTheStateSnapshot(t *testing.T) {
	data := &Order{Id: 7, OrderNo: "202607230001", Status: StatusPaid, StateVersion: 2}
	event, err := NewEventFor(data, EventOrderPaymentPaid)
	if err != nil {
		t.Fatalf("NewEventFor: %v", err)
	}
	if event.OrderId != 7 || event.OrderNo != "202607230001" || event.EventType != EventOrderPaymentPaid {
		t.Fatalf("event identity = %#v", event)
	}
	if event.PublishedAt != nil {
		t.Fatalf("a new event must start unpublished, got %v", event.PublishedAt)
	}

	var payload EventPayload
	if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.OrderNo != "202607230001" || payload.StateVersion != 2 {
		t.Fatalf("payload identity = %#v", payload)
	}
	if payload.PaymentStatus != "paid" || payload.FulfillmentStatus != "pending" {
		t.Fatalf("paid payload statuses = %#v", payload)
	}
}

func TestParseEventPayloadToleratesMissingFields(t *testing.T) {
	// Events written before a field existed must not fail to decode, otherwise a
	// stream client would drop them and silently lose state.
	payload := ParseEventPayload(`{"order_no":"legacy"}`)
	if payload.OrderNo != "legacy" {
		t.Fatalf("order_no = %q, want legacy", payload.OrderNo)
	}
	if payload.StateVersion != 0 || payload.PaymentStatus != "" {
		t.Fatalf("unset fields should stay zero, got %#v", payload)
	}

	if empty := ParseEventPayload("not json"); empty.OrderNo != "" || empty.StateVersion != 0 {
		t.Fatalf("malformed payload should decode to zero value, got %#v", empty)
	}
}

func TestNormalizeEventLimit(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{0, maxEventBatch},
		{-1, maxEventBatch},
		{1, 1},
		{500, 500},
		{maxEventBatch, maxEventBatch},
		{maxEventBatch + 1, maxEventBatch},
	}
	for _, tc := range cases {
		if got := normalizeEventLimit(tc.in); got != tc.want {
			t.Errorf("normalizeEventLimit(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
