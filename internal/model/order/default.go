package order

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/ent"
	entorder "github.com/perfect-panel/server/ent/order"
	"github.com/redis/go-redis/v9"
)

var _ Model = (*customOrderModel)(nil)
var (
	cacheOrderIdPrefix = "cache:order:id:"
	cacheOrderNoPrefix = "cache:order:no:"
)

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

//nolint:unused
func (m *defaultOrderModel) batchGetCacheKeys(Orders ...*Order) []string {
	var keys []string
	for _, order := range Orders {
		keys = append(keys, m.getCacheKeys(order)...)
	}
	return keys

}
func (m *defaultOrderModel) getCacheKeys(data *Order) []string {
	if data == nil {
		return []string{}
	}
	orderIdKey := fmt.Sprintf("%s%v", cacheOrderIdPrefix, data.Id)
	orderNoKey := fmt.Sprintf("%s%v", cacheOrderNoPrefix, data.OrderNo)
	cacheKeys := []string{
		orderIdKey,
		orderNoKey,
	}
	return cacheKeys
}

func (m *defaultOrderModel) Insert(ctx context.Context, data *Order) error {
	saved, err := m.orderCreate(data).Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToOrder(saved)
	return m.delCache(ctx, data)
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
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	if _, err = m.orderUpdate(data).Save(ctx); err != nil {
		return err
	}
	return m.delCache(ctx, old, data)
}

func (m *defaultOrderModel) Delete(ctx context.Context, id int64) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err = m.db.Order.DeleteOneID(id).Exec(ctx); err != nil {
		return err
	}
	return m.delCache(ctx, data)
}

func (m *defaultOrderModel) delCache(ctx context.Context, orders ...*Order) error {
	if m.redis == nil {
		return nil
	}
	keys := m.batchGetCacheKeys(orders...)
	if len(keys) == 0 {
		return nil
	}
	return m.redis.Del(ctx, keys...).Err()
}
