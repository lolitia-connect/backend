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

func (c *Coupon) IsEnabled() bool {
	return c != nil && c.Enable != nil && *c.Enable
}

// The editor sends milliseconds, while earlier integrations stored Unix seconds.
const millisecondTimestampFloor int64 = 100_000_000_000

func (c *Coupon) ReservableAt(now time.Time) bool {
	clock := func(timestamp int64) int64 {
		if timestamp >= millisecondTimestampFloor {
			return now.UnixMilli()
		}
		return now.Unix()
	}
	return c.IsEnabled() && c.StartTime <= clock(c.StartTime) && (c.ExpireTime <= 0 || c.ExpireTime >= clock(c.ExpireTime))
}
