package order

import (
	"context"

	entorder "github.com/perfect-panel/server/ent/order"
)

// Order state transitions.
//
// Every transition in this file does three things in one statement batch:
// it writes the new status, increments `state_version`, and appends the
// matching row to the `order_event` outbox. A transition that does not change
// the status writes no event, so a duplicated payment callback cannot make a
// client believe the order was paid twice.
//
// All of them must be called from inside `store.InTx`. The outbox row is what
// makes a state change recoverable for a reconnecting client, and it is only
// atomic with the status write while both share one transaction.

// UpdateOrderStatus writes a status transition and announces it.
//
// The write stays unconditional on the previous status, matching the behaviour
// every existing caller relies on (a late payment on a closed order still
// completes it). Only a write that actually moved the status appends an event.
func (m *customOrderModel) UpdateOrderStatus(ctx context.Context, orderNo string, status uint8) error {
	affected, err := m.db.Order.Update().
		Where(
			entorder.OrderNo(orderNo),
			entorder.StatusNEQ(status),
		).
		SetStatus(status).
		AddStateVersion(1).
		Save(ctx)
	if err != nil {
		return err
	}
	// The order does not exist, or already holds this status: no state change,
	// therefore no event.
	if affected == 0 {
		return nil
	}
	data, err := m.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return err
	}
	return m.insertEvent(ctx, data, EventTypeForStatus(status))
}

// FinishOrder completes a paid order after its activation has been committed.
// It is conditional on the order still being Paid, so a retried activation
// task cannot emit a second `order.fulfilled`.
func (m *customOrderModel) FinishOrder(ctx context.Context, orderNo string) (bool, error) {
	affected, err := m.db.Order.Update().
		Where(
			entorder.OrderNo(orderNo),
			entorder.Status(StatusPaid),
		).
		SetStatus(StatusFinished).
		AddStateVersion(1).
		Save(ctx)
	if err != nil {
		return false, err
	}
	if affected != 1 {
		return false, nil
	}
	data, err := m.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return false, err
	}
	if err := m.insertEvent(ctx, data, EventOrderFulfilled); err != nil {
		return false, err
	}
	return true, nil
}

// insertEvent appends one outbox row. It intentionally writes through the same
// ent client as the status update above, so when the caller is inside
// `store.InTx` both land or neither does.
func (m *defaultOrderModel) insertEvent(ctx context.Context, data *Order, eventType string) error {
	event, err := NewEventFor(data, eventType)
	if err != nil {
		return err
	}
	_, err = m.db.OrderEvent.Create().
		SetOrderID(event.OrderId).
		SetOrderNo(event.OrderNo).
		SetEventType(event.EventType).
		SetPayload(event.Payload).
		Save(ctx)
	return err
}
