package order

import "github.com/perfect-panel/server/ent"

func (m *defaultOrderModel) orderCreate(data *Order) *ent.OrderCreate {
	create := m.db.Order.Create().
		SetParentID(data.ParentId).
		SetUserID(data.UserId).
		SetOrderNo(data.OrderNo).
		SetType(data.Type).
		SetQuantity(data.Quantity).
		SetPrice(data.Price).
		SetAmount(data.Amount).
		SetGiftAmount(data.GiftAmount).
		SetDiscount(data.Discount).
		SetNillableCoupon(nilIfEmpty(data.Coupon)).
		SetCouponDiscount(data.CouponDiscount).
		SetCommission(data.Commission).
		SetPaymentID(data.PaymentId).
		SetMethod(data.Method).
		SetFeeAmount(data.FeeAmount).
		SetNillableTradeNo(nilIfEmpty(data.TradeNo)).
		SetStatus(data.Status).
		SetSubscribeID(data.SubscribeId).
		SetNillableSubscribeToken(nilIfEmpty(data.SubscribeToken)).
		SetIsNew(data.IsNew)
	if data.Id > 0 {
		create.SetID(data.Id)
	}
	if !data.CreatedAt.IsZero() {
		create.SetCreatedAt(data.CreatedAt)
	}
	if !data.UpdatedAt.IsZero() {
		create.SetUpdatedAt(data.UpdatedAt)
	}
	return create
}

func (m *defaultOrderModel) orderUpdate(data *Order) *ent.OrderUpdateOne {
	update := m.db.Order.UpdateOneID(data.Id).
		SetParentID(data.ParentId).
		SetUserID(data.UserId).
		SetOrderNo(data.OrderNo).
		SetType(data.Type).
		SetQuantity(data.Quantity).
		SetPrice(data.Price).
		SetAmount(data.Amount).
		SetGiftAmount(data.GiftAmount).
		SetDiscount(data.Discount).
		SetNillableCoupon(nilIfEmpty(data.Coupon)).
		SetCouponDiscount(data.CouponDiscount).
		SetCommission(data.Commission).
		SetPaymentID(data.PaymentId).
		SetMethod(data.Method).
		SetFeeAmount(data.FeeAmount).
		SetNillableTradeNo(nilIfEmpty(data.TradeNo)).
		SetStatus(data.Status).
		SetSubscribeID(data.SubscribeId).
		SetNillableSubscribeToken(nilIfEmpty(data.SubscribeToken)).
		SetIsNew(data.IsNew)
	if !data.UpdatedAt.IsZero() {
		update.SetUpdatedAt(data.UpdatedAt)
	}
	return update
}

func nilIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
