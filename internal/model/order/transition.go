package order

import (
	"context"
	"fmt"
	"strings"

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
	update := m.db.Order.Update().
		Where(entorder.OrderNo(orderNo), entorder.StatusNEQ(status)).
		SetStatus(status).AddStateVersion(1)
	if status == StatusFinished || status == StatusClosed || status == StatusFailed {
		update.ClearGuestPasswordHash().ClearGuestInviteCode()
	}
	affected, err := update.Save(ctx)
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

func (m *customOrderModel) MarkOrderPaid(ctx context.Context, orderNo, tradeNo string) (bool, error) {
	if tradeNo == "" {
		return false, fmt.Errorf("payment trade number is required")
	}
	affected, err := m.db.Order.Update().
		Where(entorder.OrderNo(orderNo), entorder.Status(StatusPending),
			entorder.Or(entorder.TradeNoIsNil(), entorder.TradeNo(""), entorder.TradeNo(tradeNo))).
		SetTradeNo(tradeNo).
		SetStatus(StatusPaid).
		AddStateVersion(1).
		Save(ctx)
	if err != nil || affected == 0 {
		return false, err
	}
	data, err := m.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return false, err
	}
	if err := m.insertEvent(ctx, data, EventOrderPaymentPaid); err != nil {
		return false, err
	}
	return true, nil
}

func (m *customOrderModel) ClosePendingOrder(ctx context.Context, orderNo string) (bool, error) {
	affected, err := m.db.Order.Update().
		Where(entorder.OrderNo(orderNo), entorder.Status(StatusPending)).
		SetStatus(StatusClosed).
		ClearGuestPasswordHash().ClearGuestInviteCode().
		SetCouponReserved(false).
		AddStateVersion(1).
		Save(ctx)
	if err != nil || affected == 0 {
		return false, err
	}
	data, err := m.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return false, err
	}
	if err := m.insertEvent(ctx, data, EventOrderClosed); err != nil {
		return false, err
	}
	return true, nil
}

func (m *customOrderModel) ClosePaidOrder(ctx context.Context, orderNo string) (bool, error) {
	affected, err := m.db.Order.Update().Where(entorder.OrderNo(orderNo), entorder.Status(StatusPaid)).
		SetStatus(StatusClosed).ClearGuestPasswordHash().ClearGuestInviteCode().SetCouponReserved(false).AddStateVersion(1).Save(ctx)
	if err != nil || affected == 0 {
		return false, err
	}
	data, err := m.FindOneByOrderNo(ctx, orderNo)
	if err != nil {
		return false, err
	}
	if err := m.insertEvent(ctx, data, EventOrderClosed); err != nil {
		return false, err
	}
	return true, nil
}

func (m *customOrderModel) SetPaymentExpectation(ctx context.Context, orderNo string, amount int64, currency string) (bool, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if amount <= 0 || currency == "" {
		return false, fmt.Errorf("invalid payment expectation")
	}
	affected, err := m.db.Order.Update().
		Where(entorder.OrderNo(orderNo), entorder.Status(StatusPending), entorder.PaymentCurrency("")).
		SetPaymentAmount(amount).SetPaymentCurrency(currency).Save(ctx)
	return affected == 1, err
}

func (m *customOrderModel) BeginPaymentSession(ctx context.Context, orderNo, session string) (bool, error) {
	if strings.TrimSpace(session) == "" {
		return false, fmt.Errorf("payment session is required")
	}
	affected, err := m.db.Order.Update().
		Where(entorder.OrderNo(orderNo), entorder.Status(StatusPending),
			entorder.Or(entorder.TradeNoIsNil(), entorder.TradeNo("")),
			entorder.Or(entorder.PaymentSessionIsNil(), entorder.PaymentSession(""))).
		SetPaymentSession(session).Save(ctx)
	return affected == 1, err
}

func (m *customOrderModel) CompletePaymentSession(ctx context.Context, orderNo, expectedSession, tradeNo, session string) (bool, error) {
	if expectedSession == "" || strings.TrimSpace(tradeNo) == "" || len(tradeNo) > 255 || strings.TrimSpace(session) == "" {
		return false, fmt.Errorf("invalid payment session completion")
	}
	affected, err := m.db.Order.Update().
		Where(entorder.OrderNo(orderNo), entorder.Status(StatusPending), entorder.PaymentSession(expectedSession),
			entorder.Or(entorder.TradeNoIsNil(), entorder.TradeNo(""), entorder.TradeNo(tradeNo))).
		SetTradeNo(tradeNo).SetPaymentSession(session).Save(ctx)
	return affected == 1, err
}

func (m *customOrderModel) ClaimPaymentSession(ctx context.Context, orderNo, tradeNo, session string) (bool, error) {
	if tradeNo == "" {
		return false, fmt.Errorf("payment trade number is required")
	}
	affected, err := m.db.Order.Update().
		Where(entorder.OrderNo(orderNo), entorder.Status(StatusPending),
			entorder.Or(entorder.TradeNoIsNil(), entorder.TradeNo(""), entorder.TradeNo(tradeNo))).
		SetTradeNo(tradeNo).SetPaymentSession(session).Save(ctx)
	return affected == 1, err
}

func (m *customOrderModel) SetCommission(ctx context.Context, id, amount, refererID int64) error {
	return m.db.Order.UpdateOneID(id).SetCommission(amount).SetCommissionRefererID(refererID).Exec(ctx)
}

// FinishOrder completes a paid order after its activation has been committed.
// It is conditional on the order still being Paid, so a retried activation
// task cannot emit a second `order.fulfilled`.
func (m *customOrderModel) FinishOrder(ctx context.Context, orderNo string, recalculateGroup bool) (bool, error) {
	affected, err := m.db.Order.Update().
		Where(
			entorder.OrderNo(orderNo),
			entorder.Status(StatusPaid),
		).
		SetStatus(StatusFinished).
		ClearGuestPasswordHash().ClearGuestInviteCode().
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
	event, err := newEventFor(data, EventOrderFulfilled, recalculateGroup)
	if err != nil {
		return false, err
	}
	if err := newEventModel(m.db).Insert(ctx, event); err != nil {
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
