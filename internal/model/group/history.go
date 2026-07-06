package group

import "time"

// GroupHistory 分组历史记录模型
type GroupHistory struct {
	Id           int64
	GroupMode    string
	TriggerType  string
	State        string
	TotalUsers   int
	SuccessCount int
	FailedCount  int
	StartTime    *time.Time
	EndTime      *time.Time
	Operator     string
	ErrorMessage string
	CreatedAt    time.Time
}

// GroupHistoryDetail 分组历史详情模型
type GroupHistoryDetail struct {
	Id          int64
	HistoryId   int64
	NodeGroupId int64
	UserCount   int
	NodeCount   int
	UserData    string
	CreatedAt   time.Time
}
