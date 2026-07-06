package system

import "time"

type System struct {
	Id        int64
	Category  string
	Key       string
	Value     string
	Type      string
	Desc      string
	CreatedAt time.Time
	UpdatedAt time.Time
}
