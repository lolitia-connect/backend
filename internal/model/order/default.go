package order

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/ent"
	entorder "github.com/perfect-panel/server/ent/order"
	"github.com/redis/go-redis/v9"
)

var _ Model = (*customOrderModel)(nil)

type (
	Model interface {
		orderModel
		customOrderLogicModel
	}
	orderModel interface {
		Insert(ctx context.Context, data *Order) error
		FindOne(ctx context.Context, id int64) (*Order, error)
		FindOneByOrderNo(ctx context.Context, orderNo string) (*Order, error)
		FindOneByIdempotencyKey(ctx context.Context, key string) (*Order, error)
		Update(ctx context.Context, data *Order) error
		Delete(ctx context.Context, id int64) error
	}

	customOrderModel struct {
		*defaultOrderModel
	}
	defaultOrderModel struct {
		db    *ent.Client
		redis *redis.Client
		table string
	}
)

func newOrderModel(db *ent.Client, c *redis.Client) *defaultOrderModel {
	return &defaultOrderModel{
		db:    db,
		redis: c,
		table: "order",
	}
}

func (m *defaultOrderModel) Insert(ctx context.Context, data *Order) error {
	// The first version of an order is always 1, so a client can tell a freshly
	// created order apart from one whose version was never initialised. Ent
	// requires an explicit value here because the column default only applies to
	// rows written outside the model.
	if data.StateVersion == 0 {
		data.StateVersion = 1
	}
	saved, err := m.orderCreate(data).Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToOrder(saved)
	// Announce the creation in the same transaction, so an order that was
	// committed is always visible on the event stream.
	return m.insertEvent(ctx, data, EventOrderCreated)
}

func (m *defaultOrderModel) FindOne(ctx context.Context, id int64) (*Order, error) {
	data, err := m.db.Order.Get(ctx, id)
	return entToOrder(data), err
}

func (m *defaultOrderModel) FindOneByOrderNo(ctx context.Context, orderNo string) (*Order, error) {
	data, err := m.db.Order.Query().Where(entorder.OrderNo(orderNo)).First(ctx)
	return entToOrder(data), err
}

// FindOneByIdempotencyKey resolves the order a V2 client already created with
// this key. V1 orders never set the column, so an empty key matches nothing.
func (m *defaultOrderModel) FindOneByIdempotencyKey(ctx context.Context, key string) (*Order, error) {
	if key == "" {
		return nil, &ent.NotFoundError{}
	}
	data, err := m.db.Order.Query().Where(entorder.IdempotencyKey(key)).First(ctx)
	return entToOrder(data), err
}

func (m *defaultOrderModel) Update(ctx context.Context, data *Order) error {
	existing, err := m.FindOne(ctx, data.Id)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	// A status change must go through a transition, so that it is versioned and
	// announced on the order event stream. Rejecting it here turns a silently
	// unannounced state change into a visible error instead of an order that no
	// client ever learns about.
	if existing != nil && existing.Status != data.Status {
		return fmt.Errorf("order %s status may only change through a state transition", data.OrderNo)
	}
	if _, err = m.orderUpdate(data).Save(ctx); err != nil {
		return err
	}
	return nil
}

func (m *defaultOrderModel) Delete(ctx context.Context, id int64) error {
	_, err := m.FindOne(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err = m.db.Order.DeleteOneID(id).Exec(ctx); err != nil {
		return err
	}
	return nil
}
