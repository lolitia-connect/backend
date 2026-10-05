package order

import (
	"context"
	"encoding/json"
	"time"

	"github.com/perfect-panel/server/ent"
)

// Order status codes, matching the values already written by every transition
// site in this repository.
const (
	StatusPending  uint8 = 1
	StatusPaid     uint8 = 2
	StatusClosed   uint8 = 3
	StatusFailed   uint8 = 4
	StatusFinished uint8 = 5
)

// Durable order event types. The set is fixed: a queue retry must never
// produce a new type, and "still processing" is represented by the absence of
// a terminal event, not by a failure event.
const (
	EventOrderCreated     = "order.created"
	EventOrderPaymentPaid = "order.payment_paid"
	EventOrderFulfilled   = "order.fulfilled"
	EventOrderClosed      = "order.closed"
	EventOrderStateChange = "order.state_changed"
)

// EventTypeForStatus maps an order status to the event that announces reaching
// it. A transition into a status with no dedicated event still gets an
// `order.state_changed` row so the stream never misses a state change.
func EventTypeForStatus(status uint8) string {
	switch status {
	case StatusPaid:
		return EventOrderPaymentPaid
	case StatusClosed:
		return EventOrderClosed
	case StatusFinished:
		return EventOrderFulfilled
	default:
		return EventOrderStateChange
	}
}

// PaymentStatus is the coarse payment leg of an order, as published to
// clients. It is derived from the status and never stored separately.
func PaymentStatus(status uint8) string {
	switch status {
	case StatusPaid, StatusFinished:
		return "paid"
	case StatusClosed:
		return "closed"
	case StatusFailed:
		return "failed"
	default:
		return "pending"
	}
}

// FulfillmentStatus is the fulfillment leg of an order. Paid-but-not-yet-
// activated orders report "pending" so a client can tell the difference
// between "money received" and "subscription usable".
func FulfillmentStatus(status uint8) string {
	switch status {
	case StatusFinished:
		return "finished"
	case StatusPaid:
		return "pending"
	default:
		return "not_started"
	}
}

// Event is one row of the order_event transactional outbox. It is only ever
// written inside the same transaction as the state change it announces, so a
// committed state change always has a recoverable event and a rolled-back one
// never does.
type Event struct {
	Id          int64
	OrderId     int64
	OrderNo     string
	EventType   string
	Payload     string
	CreatedAt   time.Time
	PublishedAt *time.Time
}

// EventPayload is the JSON body broadcast to clients. It carries the status
// snapshot as of the event, so a client that missed earlier events can still
// derive its local state from the newest one it received.
type EventPayload struct {
	OrderNo           string `json:"order_no"`
	StateVersion      int64  `json:"state_version"`
	PaymentStatus     string `json:"payment_status"`
	FulfillmentStatus string `json:"fulfillment_status"`
}

// NewEventFor builds the outbox row for one state change of an order. The
// caller must persist it in the same transaction as the status write.
func NewEventFor(data *Order, eventType string) (*Event, error) {
	payload, err := json.Marshal(EventPayload{
		OrderNo:           data.OrderNo,
		StateVersion:      data.StateVersion,
		PaymentStatus:     PaymentStatus(data.Status),
		FulfillmentStatus: FulfillmentStatus(data.Status),
	})
	if err != nil {
		return nil, err
	}
	return &Event{
		OrderId:   data.Id,
		OrderNo:   data.OrderNo,
		EventType: eventType,
		Payload:   string(payload),
	}, nil
}

// ParseEventPayload decodes an outbox payload. Events written by an older
// version only lack fields, so a partial decode is not an error.
func ParseEventPayload(payload string) EventPayload {
	var decoded EventPayload
	_ = json.Unmarshal([]byte(payload), &decoded)
	return decoded
}

// EventModel is the outbox store. It is deliberately separate from the order
// model: order mutations write these rows atomically, while delivery workers
// and stream handlers only need to read them and mark them published.
type EventModel interface {
	Insert(ctx context.Context, event *Event) error
	FindOne(ctx context.Context, id int64) (*Event, error)
	// ListAfter returns up to limit events of one order with id > afterId, in
	// ascending id order, for stream replay.
	ListAfter(ctx context.Context, orderNo string, afterId int64, limit int) ([]*Event, error)
	// EarliestID returns the lowest retained event id of an order, or 0 when no
	// event is retained. A replay cursor older than this cannot be served from
	// the outbox alone.
	EarliestID(ctx context.Context, orderNo string) (int64, error)
	// ListUnpublished returns events whose Redis broadcast has not been
	// confirmed yet, oldest first.
	ListUnpublished(ctx context.Context, limit int) ([]*Event, error)
	// MarkPublished records a confirmed broadcast. The update is conditional on
	// published_at still being NULL, so a concurrent publisher cannot count the
	// same row twice.
	MarkPublished(ctx context.Context, id int64, publishedAt time.Time) (bool, error)
	// DeletePublishedBefore removes events that are both published and past the
	// replay retention window. Unpublished events are never removed.
	DeletePublishedBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// maxEventBatch bounds every outbox read. The publisher and the stream handler
// both chunk through the backlog rather than loading an unbounded slice.
const maxEventBatch = 1000

func normalizeEventLimit(limit int) int {
	if limit <= 0 || limit > maxEventBatch {
		return maxEventBatch
	}
	return limit
}

func entToEvent(data *ent.OrderEvent) *Event {
	if data == nil {
		return nil
	}
	return &Event{
		Id:          data.ID,
		OrderId:     data.OrderID,
		OrderNo:     data.OrderNo,
		EventType:   data.EventType,
		Payload:     data.Payload,
		CreatedAt:   data.CreatedAt,
		PublishedAt: data.PublishedAt,
	}
}

func entEventsToEvents(list []*ent.OrderEvent) []*Event {
	items := make([]*Event, 0, len(list))
	for _, item := range list {
		items = append(items, entToEvent(item))
	}
	return items
}
