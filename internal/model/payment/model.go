package payment

import (
	"context"

	"github.com/perfect-panel/server/ent"
	entpayment "github.com/perfect-panel/server/ent/payment"
)

type customPaymentLogicModel interface {
	FindOneByPaymentToken(ctx context.Context, token string) (*Payment, error)
	FindAll(ctx context.Context) ([]*Payment, error)
	FindListByPage(ctx context.Context, page, size int, req *Filter) (int64, []*Payment, error)
	FindAvailableMethods(ctx context.Context) ([]*Payment, error)
}

// NewModel returns a model for the database table.
func NewModel(conn *ent.Client) Model {
	return &customPaymentModel{
		defaultPaymentModel: newPaymentModel(conn),
	}
}

func (m *customPaymentModel) FindOneByPaymentToken(ctx context.Context, token string) (*Payment, error) {
	data, err := m.db.Payment.Query().Where(entpayment.Token(token)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return paymentFromEnt(data), nil
}

func (m *customPaymentModel) FindAll(ctx context.Context) ([]*Payment, error) {
	items, err := m.db.Payment.Query().Order(ent.Asc(entpayment.FieldSort), ent.Asc(entpayment.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	return paymentsFromEnt(items), nil
}

func (m *customPaymentModel) FindAvailableMethods(ctx context.Context) ([]*Payment, error) {
	items, err := m.db.Payment.Query().Where(entpayment.Enable(true)).Order(ent.Asc(entpayment.FieldSort), ent.Asc(entpayment.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	return paymentsFromEnt(items), nil
}

func (m *customPaymentModel) FindListByPage(ctx context.Context, page, size int, req *Filter) (int64, []*Payment, error) {
	query := m.db.Payment.Query()
	if req != nil {
		if req.Enable != nil {
			query = query.Where(entpayment.Enable(*req.Enable))
		}
		if req.Mark != "" {
			query = query.Where(entpayment.Platform(req.Mark))
		}
		if req.Search != "" {
			query = query.Where(entpayment.NameHasPrefix(req.Search))
		}
	}
	total, err := query.Count(ctx)
	if err != nil {
		return 0, nil, err
	}
	items, err := query.Order(ent.Asc(entpayment.FieldSort), ent.Asc(entpayment.FieldID)).Offset((page - 1) * size).Limit(size).All(ctx)
	if err != nil {
		return 0, nil, err
	}
	return int64(total), paymentsFromEnt(items), nil
}

func paymentsFromEnt(items []*ent.Payment) []*Payment {
	list := make([]*Payment, 0, len(items))
	for _, item := range items {
		list = append(list, paymentFromEnt(item))
	}
	return list
}
