package order

import (
	"time"
)

type Order struct {
	Id             int64
	ParentId       int64
	UserId         int64
	OrderNo        string
	Type           uint8
	Quantity       int64
	Price          int64
	Amount         int64
	GiftAmount     int64
	Discount       int64
	Coupon         string
	CouponDiscount int64
	Commission     int64
	PaymentId      int64
	Method         string
	FeeAmount      int64
	TradeNo        string
	Status         uint8
	SubscribeId    int64
	SubscribeToken string
	IsNew          bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type OrdersTotal struct {
	AmountTotal        int64
	NewOrderAmount     int64
	RenewalOrderAmount int64
}
