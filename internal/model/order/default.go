package order

import (
	"context"

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
	saved, err := m.orderCreate(data).Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToOrder(saved)
	return nil
}

func (m *defaultOrderModel) FindOne(ctx context.Context, id int64) (*Order, error) {
	data, err := m.db.Order.Get(ctx, id)
	return entToOrder(data), err
}

func (m *defaultOrderModel) FindOneByOrderNo(ctx context.Context, orderNo string) (*Order, error) {
	data, err := m.db.Order.Query().Where(entorder.OrderNo(orderNo)).First(ctx)
	return entToOrder(data), err
}

func (m *defaultOrderModel) Update(ctx context.Context, data *Order) error {
	_, err := m.FindOne(ctx, data.Id)
	if err != nil && !ent.IsNotFound(err) {
		return err
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
