package group

import (
	"time"

	"gorm.io/gorm"
)

// NodeGroup 节点组模型
type NodeGroup struct {
	Id                  int64     `gorm:"primaryKey"`
	Name                string    `gorm:"type:varchar(255);not null;comment:Name"`
	Description         string    `gorm:"type:varchar(500);comment:Description"`
	Sort                int       `gorm:"default:0;index:idx_sort;comment:Sort Order"`
	ForCalculation      *bool     `gorm:"default:true;not null;comment:For Calculation: whether this node group participates in grouping calculation"`
	IsExpiredGroup      *bool     `gorm:"default:false;not null;index:idx_is_expired_group;comment:Is Expired Group"`
	ExpiredDaysLimit    int       `gorm:"default:7;not null;comment:Expired days limit (days)"`
	MaxTrafficGBExpired *int64    `gorm:"default:0;comment:Max traffic for expired users (GB)"`
	SpeedLimit          int       `gorm:"default:0;not null;comment:Speed limit (KB/s)"`
	MinTrafficGB        *int64    `gorm:"default:0;comment:Minimum Traffic (GB) for this node group"`
	MaxTrafficGB        *int64    `gorm:"default:0;comment:Maximum Traffic (GB) for this node group"`
	CreatedAt           time.Time `gorm:"<-:create;comment:Create Time"`
	UpdatedAt           time.Time `gorm:"comment:Update Time"`
}

// TableName 指定表名
func (*NodeGroup) TableName() string {
	return "node_group"
}

// BeforeCreate GORM hook - 创建前回调
func (ng *NodeGroup) BeforeCreate(tx *gorm.DB) error {
	return nil
}
