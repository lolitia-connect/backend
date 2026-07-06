package group

import "time"

// NodeGroup 节点组模型
type NodeGroup struct {
	Id                  int64
	Name                string
	Type                string
	Description         string
	Sort                int
	ForCalculation      *bool
	IsExpiredGroup      *bool
	ExpiredDaysLimit    int
	MaxTrafficGBExpired *int64
	SpeedLimit          int
	MinTrafficGB        *int64
	MaxTrafficGB        *int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
