package coupon

import "time"

type Coupon struct {
	Id         int64
	Name       string
	Code       string
	Count      int64
	Type       uint8
	Discount   int64
	StartTime  int64
	ExpireTime int64
	UserLimit  int64
	Subscribe  string
	UsedCount  int64
	Enable     *bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (Coupon) TableName() string {
	return "coupon"
}

func (c *Coupon) IsEnabled() bool {
	return c != nil && c.Enable != nil && *c.Enable
}
