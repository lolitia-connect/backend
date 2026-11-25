package coupon

import (
	"context"
	"fmt"
	"strconv"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	entcoupon "github.com/perfect-panel/server/ent/coupon"
)

type customCouponLogicModel interface {
	UpdateCount(ctx context.Context, code string) error
	ReserveUsage(ctx context.Context, code string) error
	ReleaseUsage(ctx context.Context, code string) error
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
	affected, err := m.db.Coupon.Update().Where(entcoupon.Code(code)).AddUsedCount(1).Save(ctx)
	if err != nil {
		return err
	}
	if affected == 0 {
		return &ent.NotFoundError{}
	}
	return nil
}

func (m *customCouponModel) ReserveUsage(ctx context.Context, code string) error {
	now := time.Now()
	started := entcoupon.Or(entcoupon.StartTimeLTE(now.Unix()),
		entcoupon.And(entcoupon.StartTimeGTE(millisecondTimestampFloor), entcoupon.StartTimeLTE(now.UnixMilli())))
	unexpired := entcoupon.Or(entcoupon.ExpireTime(0), entcoupon.ExpireTimeGTE(now.UnixMilli()),
		entcoupon.And(entcoupon.ExpireTimeLT(millisecondTimestampFloor), entcoupon.ExpireTimeGTE(now.Unix())))
	affected, err := m.db.Coupon.Update().Where(entcoupon.Code(code), entcoupon.Enable(true), started, unexpired,
		func(selector *entsql.Selector) {
			selector.Where(entsql.Or(entsql.EQ(selector.C(entcoupon.FieldCount), 0),
				entsql.ColumnsLT(selector.C(entcoupon.FieldUsedCount), selector.C(entcoupon.FieldCount))))
		}).AddUsedCount(1).Save(ctx)
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("coupon is unavailable or exhausted")
	}
	return nil
}

func (m *customCouponModel) ReleaseUsage(ctx context.Context, code string) error {
	_, err := m.db.Coupon.Update().Where(entcoupon.Code(code), entcoupon.UsedCountGT(0)).AddUsedCount(-1).Save(ctx)
	return err
}
