package order

import (
	"time"

	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/model/subscribe"
)

type Details struct {
	Id                  int64
	ParentId            int64
	SubOrders           []*Order
	UserId              int64
	OrderNo             string
	Type                uint8
	Quantity            int64
	Price               int64
	Amount              int64
	Discount            int64
	Coupon              string
	CouponDiscount      int64
	PaymentId           int64
	Payment             *payment.Payment
	Method              string
	FeeAmount           int64
	TradeNo             string
	GiftAmount          int64
	Commission          int64
	CommissionRefererId int64
	Status              uint8
	SubscribeId         int64
	SubscribeToken      string
	UserSubscribeId     int64
	Subscribe           *subscribe.Subscribe
	IsNew               bool
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type OrdersTotalWithDate struct {
	Date               string
	AmountTotal        int64
	NewOrderAmount     int64
	RenewalOrderAmount int64
}

func (o *Order) IsPaidRenewal() bool {
	return o != nil && o.Type == 2 && (o.Status == StatusPaid || o.Status == StatusFinished)
}

func (d *Details) RefundBasis() int64 {
	basis := d.Amount + d.GiftAmount
	for _, renewal := range d.SubOrders {
		if renewal.IsPaidRenewal() {
			basis += renewal.Amount + renewal.GiftAmount
		}
	}
	return basis
}

func (d *Details) RefundGiftBasis() int64 {
	gift := d.GiftAmount
	for _, renewal := range d.SubOrders {
		if renewal.IsPaidRenewal() {
			gift += renewal.GiftAmount
		}
	}
	return gift
}
