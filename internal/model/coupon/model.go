package coupon

import (
	"context"
	"strconv"

	"github.com/perfect-panel/server/ent"
	entcoupon "github.com/perfect-panel/server/ent/coupon"
)

type customCouponLogicModel interface {
	UpdateCount(ctx context.Context, code string) error
	QueryCouponListByPage(ctx context.Context, page, size int, subscribe int64, search string) (total int64, list []*Coupon, err error)
	BatchDelete(ctx context.Context, ids []int64) error
}

// NewModel returns a model for the database table.
func NewModel(conn *ent.Client) Model {
	return &customCouponModel{
		defaultCouponModel: newCouponModel(conn),
	}
}

// QueryCouponListByPage query coupon list by page
func (m *customCouponModel) QueryCouponListByPage(ctx context.Context, page, size int, subscribe int64, search string) (total int64, list []*Coupon, err error) {
	query := m.db.Coupon.Query()
	if subscribe != 0 {
		tag := strconv.FormatInt(subscribe, 10)
		query = query.Where(entcoupon.Or(entcoupon.Subscribe(tag), entcoupon.SubscribeHasPrefix(tag+","), entcoupon.SubscribeHasSuffix(","+tag), entcoupon.SubscribeContains(","+tag+",")))
	}
	if search != "" {
		query = query.Where(entcoupon.Or(entcoupon.NameHasPrefix(search), entcoupon.CodeHasPrefix(search)))
	}
	count, err := query.Count(ctx)
	if err != nil {
		return 0, nil, err
	}
	items, err := query.Limit(size).Offset((page - 1) * size).All(ctx)
	if err != nil {
		return 0, nil, err
	}
	list = make([]*Coupon, 0, len(items))
	for _, item := range items {
		list = append(list, couponFromEnt(item))
	}
	return int64(count), list, nil
}

func (m *customCouponModel) BatchDelete(ctx context.Context, ids []int64) error {
	var err error
	for _, id := range ids {
		if err = m.Delete(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (m *customCouponModel) UpdateCount(ctx context.Context, code string) error {
	data, err := m.FindOneByCode(ctx, code)
	if err != nil {
		return err
	}
	data.UsedCount++
	return m.Update(ctx, data)
}
