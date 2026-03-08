package group

import (
	"time"

	"gorm.io/gorm"
)

// GroupHistory 分组历史记录模型
type GroupHistory struct {
	Id           int64      `gorm:"primaryKey"`
	GroupMode    string     `gorm:"type:varchar(50);not null;index:idx_group_mode;comment:Group Mode: average/subscribe/traffic"`
	TriggerType  string     `gorm:"type:varchar(50);not null;index:idx_trigger_type;comment:Trigger Type: manual/auto/schedule"`
	State        string     `gorm:"type:varchar(50);not null;index:idx_state;comment:State: pending/running/completed/failed"`
	TotalUsers   int        `gorm:"default:0;not null;comment:Total Users"`
	SuccessCount int        `gorm:"default:0;not null;comment:Success Count"`
	FailedCount  int        `gorm:"default:0;not null;comment:Failed Count"`
	StartTime    *time.Time `gorm:"comment:Start Time"`
	EndTime      *time.Time `gorm:"comment:End Time"`
	Operator     string     `gorm:"type:varchar(100);comment:Operator"`
	ErrorMessage string     `gorm:"type:TEXT;comment:Error Message"`
	CreatedAt    time.Time  `gorm:"<-:create;index:idx_created_at;comment:Create Time"`
}

// TableName 指定表名
func (*GroupHistory) TableName() string {
	return "group_history"
}

// BeforeCreate GORM hook - 创建前回调
func (gh *GroupHistory) BeforeCreate(tx *gorm.DB) error {
	return nil
}

// GroupHistoryDetail 分组历史详情模型
type GroupHistoryDetail struct {
	Id          int64     `gorm:"primaryKey"`
	HistoryId   int64     `gorm:"not null;index:idx_history_id;comment:History ID"`
	NodeGroupId int64     `gorm:"not null;index:idx_node_group_id;comment:Node Group ID"`
	UserCount   int       `gorm:"default:0;not null;comment:User Count"`
	NodeCount   int       `gorm:"default:0;not null;comment:Node Count"`
	UserData    string    `gorm:"type:text;comment:User data JSON (id and email/phone)"`
	CreatedAt   time.Time `gorm:"<-:create;comment:Create Time"`
}

// TableName 指定表名
func (*GroupHistoryDetail) TableName() string {
	return "group_history_detail"
}

// BeforeCreate GORM hook - 创建前回调
func (ghd *GroupHistoryDetail) BeforeCreate(tx *gorm.DB) error {
	return nil
}
