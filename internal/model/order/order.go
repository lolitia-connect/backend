package order

import (
	"time"
)

// Order types. Only a new subscription purchase reserves plan inventory, so
// the distinction matters to the close-order path.
const (
	TypeSubscribe    uint8 = 1
	TypeRenewal      uint8 = 2
	TypeResetTraffic uint8 = 3
	TypeRecharge     uint8 = 4
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
	StateVersion   int64

	// IdempotencyKey and IdempotencyHash are V2-only. V1 and historical orders
	// leave both empty; the unique index tolerates repeated NULLs.
	IdempotencyKey  string
	IdempotencyHash string

	// GuestCheckoutTokenHash is the durable half of the guest checkout
	// capability. Only guest orders set it, so an empty value means "this order
	// can only be authorized as its owner's account".
	GuestCheckoutTokenHash string
	SubscribeId            int64
	SubscribeToken         string
	IsNew                  bool
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type OrdersTotal struct {
	AmountTotal        int64
	NewOrderAmount     int64
	RenewalOrderAmount int64
}
