package payment

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/ent"
	entpayment "github.com/perfect-panel/server/ent/payment"
)

var _ Model = (*customPaymentModel)(nil)
var (
	cachePaymentIdPrefix    = "cache:payment:id:"
	cachePaymentTokenPrefix = "cache:payment:token:"
)

type (
	Model interface {
		paymentModel
		customPaymentLogicModel
	}
	paymentModel interface {
		Insert(ctx context.Context, data *Payment) error
		FindOne(ctx context.Context, id int64) (*Payment, error)
		Update(ctx context.Context, data *Payment) error
		Delete(ctx context.Context, id int64) error
	}

	customPaymentModel struct {
		*defaultPaymentModel
	}
	defaultPaymentModel struct {
		db *ent.Client
	}
)

func newPaymentModel(db *ent.Client) *defaultPaymentModel {
	return &defaultPaymentModel{
		db: db,
	}
}

func (m *defaultPaymentModel) Insert(ctx context.Context, data *Payment) error {
	if data.Sort == 0 {
		last, err := m.db.Payment.Query().Order(ent.Desc(entpayment.FieldSort)).First(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return err
		}
		if last != nil {
			data.Sort = last.Sort + 1
		} else {
			data.Sort = 1
		}
	}
	created, err := m.db.Payment.Create().
		SetName(data.Name).
		SetPlatform(data.Platform).
		SetIcon(data.Icon).
		SetDomain(data.Domain).
		SetConfig(data.Config).
		SetDescription(data.Description).
		SetFeeMode(data.FeeMode).
		SetFeePercent(data.FeePercent).
		SetFeeAmount(data.FeeAmount).
		SetSort(data.Sort).
		SetEnable(value(data.Enable)).
		SetToken(data.Token).
		SetCurrencyUnit(data.CurrencyUnit).
		SetExchangeRate(data.ExchangeRate).
		SetBillDesc(data.BillDesc).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, created)
	return nil
}

func (m *defaultPaymentModel) FindOne(ctx context.Context, id int64) (*Payment, error) {
	data, err := m.db.Payment.Query().Where(entpayment.ID(id)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return paymentFromEnt(data), nil
}

func (m *defaultPaymentModel) Update(ctx context.Context, data *Payment) error {
	updated, err := m.db.Payment.UpdateOneID(data.Id).
		SetName(data.Name).
		SetPlatform(data.Platform).
		SetIcon(data.Icon).
		SetDomain(data.Domain).
		SetConfig(data.Config).
		SetDescription(data.Description).
		SetFeeMode(data.FeeMode).
		SetFeePercent(data.FeePercent).
		SetFeeAmount(data.FeeAmount).
		SetSort(data.Sort).
		SetEnable(value(data.Enable)).
		SetToken(data.Token).
		SetCurrencyUnit(data.CurrencyUnit).
		SetExchangeRate(data.ExchangeRate).
		SetBillDesc(data.BillDesc).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, updated)
	return nil
}

func (m *defaultPaymentModel) Delete(ctx context.Context, id int64) error {
	if id == -1 {
		return fmt.Errorf("can't delete default payment method")
	}
	err := m.db.Payment.DeleteOneID(id).Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

func paymentFromEnt(data *ent.Payment) *Payment {
	if data == nil {
		return nil
	}
	var resp Payment
	copyFromEnt(&resp, data)
	return &resp
}

func copyFromEnt(dst *Payment, src *ent.Payment) {
	dst.Id = src.ID
	dst.Name = src.Name
	dst.Platform = src.Platform
	dst.Icon = src.Icon
	dst.Domain = src.Domain
	dst.Config = src.Config
	dst.Description = src.Description
	dst.FeeMode = src.FeeMode
	dst.FeePercent = src.FeePercent
	dst.FeeAmount = src.FeeAmount
	dst.Sort = src.Sort
	dst.Enable = ptr(src.Enable)
	dst.Token = src.Token
	dst.CurrencyUnit = src.CurrencyUnit
	dst.ExchangeRate = src.ExchangeRate
	dst.BillDesc = src.BillDesc
}

func ptr(v bool) *bool { return &v }

func value(v *bool) bool {
	return v != nil && *v
}
