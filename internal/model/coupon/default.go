package coupon

import (
	"context"

	"github.com/perfect-panel/server/ent"
	entcoupon "github.com/perfect-panel/server/ent/coupon"
)

var _ Model = (*customCouponModel)(nil)
var (
	cacheCouponIdPrefix   = "cache:coupon:id:"
	cacheCouponCodePrefix = "cache:coupon:code:"
)

type (
	Model interface {
		couponModel
		customCouponLogicModel
	}
	couponModel interface {
		Insert(ctx context.Context, data *Coupon) error
		FindOne(ctx context.Context, id int64) (*Coupon, error)
		FindOneByCode(ctx context.Context, code string) (*Coupon, error)
		Update(ctx context.Context, data *Coupon) error
		Delete(ctx context.Context, id int64) error
	}

	customCouponModel struct {
		*defaultCouponModel
	}
	defaultCouponModel struct {
		db *ent.Client
	}
)

func newCouponModel(db *ent.Client) *defaultCouponModel {
	return &defaultCouponModel{
		db: db,
	}
}

func (m *defaultCouponModel) Insert(ctx context.Context, data *Coupon) error {
	created, err := m.db.Coupon.Create().
		SetName(data.Name).
		SetCode(data.Code).
		SetCount(data.Count).
		SetType(data.Type).
		SetDiscount(data.Discount).
		SetStartTime(data.StartTime).
		SetExpireTime(data.ExpireTime).
		SetUserLimit(data.UserLimit).
		SetSubscribe(data.Subscribe).
		SetUsedCount(data.UsedCount).
		SetEnable(value(data.Enable)).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, created)
	return nil
}

func (m *defaultCouponModel) FindOne(ctx context.Context, id int64) (*Coupon, error) {
	data, err := m.db.Coupon.Query().Where(entcoupon.ID(id)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return couponFromEnt(data), nil
}

func (m *defaultCouponModel) FindOneByCode(ctx context.Context, code string) (*Coupon, error) {
	data, err := m.db.Coupon.Query().Where(entcoupon.Code(code)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return couponFromEnt(data), nil
}

func (m *defaultCouponModel) Update(ctx context.Context, data *Coupon) error {
	updated, err := m.db.Coupon.UpdateOneID(data.Id).
		SetName(data.Name).
		SetCode(data.Code).
		SetCount(data.Count).
		SetType(data.Type).
		SetDiscount(data.Discount).
		SetStartTime(data.StartTime).
		SetExpireTime(data.ExpireTime).
		SetUserLimit(data.UserLimit).
		SetSubscribe(data.Subscribe).
		SetUsedCount(data.UsedCount).
		SetEnable(value(data.Enable)).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, updated)
	return nil
}

func (m *defaultCouponModel) Delete(ctx context.Context, id int64) error {
	err := m.db.Coupon.DeleteOneID(id).Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

func couponFromEnt(data *ent.Coupon) *Coupon {
	if data == nil {
		return nil
	}
	var resp Coupon
	copyFromEnt(&resp, data)
	return &resp
}

func copyFromEnt(dst *Coupon, src *ent.Coupon) {
	dst.Id = src.ID
	dst.Name = src.Name
	dst.Code = src.Code
	dst.Count = src.Count
	dst.Type = src.Type
	dst.Discount = src.Discount
	dst.StartTime = src.StartTime
	dst.ExpireTime = src.ExpireTime
	dst.UserLimit = src.UserLimit
	dst.Subscribe = src.Subscribe
	dst.UsedCount = src.UsedCount
	dst.Enable = ptr(src.Enable)
	dst.CreatedAt = src.CreatedAt
	dst.UpdatedAt = src.UpdatedAt
}

func ptr(v bool) *bool { return &v }

func value(v *bool) bool {
	return v == nil || *v
}
