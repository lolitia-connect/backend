package node

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/perfect-panel/server/pkg/logger"
	"gorm.io/gorm"
)

// JSONInt64Slice is a custom type for handling []int64 as JSON in database
type JSONInt64Slice []int64

// Scan implements sql.Scanner interface
func (j *JSONInt64Slice) Scan(value interface{}) error {
	if value == nil {
		*j = []int64{}
		return nil
	}

	// Handle []byte
	bytes, ok := value.([]byte)
	if !ok {
		// Try to handle string
		str, ok := value.(string)
		if !ok {
			*j = []int64{}
			return nil
		}
		bytes = []byte(str)
	}

	if len(bytes) == 0 {
		*j = []int64{}
		return nil
	}

	// Check if it's a JSON array
	if bytes[0] != '[' {
		// Not a JSON array, return empty slice
		*j = []int64{}
		return nil
	}

	return json.Unmarshal(bytes, j)
}

// Value implements driver.Valuer interface
func (j JSONInt64Slice) Value() (driver.Value, error) {
	if len(j) == 0 {
		return "[]", nil
	}
	return json.Marshal(j)
}

type Node struct {
	Id           int64          `gorm:"primary_key"`
	Name         string         `gorm:"type:varchar(100);not null;default:'';comment:Node Name"`
	Tags         string         `gorm:"type:varchar(255);not null;default:'';comment:Tags"`
	Port         uint16         `gorm:"not null;default:0;comment:Connect Port"`
	Address      string         `gorm:"type:varchar(255);not null;default:'';comment:Connect Address"`
	ServerId     int64          `gorm:"not null;default:0;comment:Server ID"`
	Server       *Server        `gorm:"foreignKey:ServerId;references:Id"`
	Protocol     string         `gorm:"type:varchar(100);not null;default:'';comment:Protocol"`
	Enabled      *bool          `gorm:"type:boolean;not null;default:true;comment:Enabled"`
	NodeType     string         `gorm:"type:varchar(20);not null;default:'landing';comment:Node Type - front: frontend node, landing: landing node"`
	IsHidden     *bool          `gorm:"type:boolean;not null;default:false;comment:Hidden - users cannot see hidden nodes"`
	Sort         int            `gorm:"uniqueIndex;not null;default:0;comment:Sort"`
	NodeGroupIds JSONInt64Slice `gorm:"type:json;comment:Node Group IDs (JSON array, multiple groups)"`
	CreatedAt    time.Time      `gorm:"<-:create;comment:Creation Time"`
	UpdatedAt    time.Time      `gorm:"comment:Update Time"`
}

func (n *Node) TableName() string {
	return "nodes"
}

func (n *Node) BeforeCreate(tx *gorm.DB) error {
	if n.Sort == 0 {
		var maxSort int
		if err := tx.Model(&Node{}).Select("COALESCE(MAX(sort), 0)").Scan(&maxSort).Error; err != nil {
			return err
		}
		n.Sort = maxSort + 1
	}
	return nil
}

func (n *Node) BeforeDelete(tx *gorm.DB) error {
	if err := tx.Exec("UPDATE `nodes` SET sort = sort - 1 WHERE sort > ?", n.Sort).Error; err != nil {
		return err
	}
	return nil
}

func (n *Node) BeforeUpdate(tx *gorm.DB) error {
	var count int64
	if err := tx.Set("gorm:query_option", "FOR UPDATE").Model(&Server{}).
		Where("sort = ? AND id != ?", n.Sort, n.Id).Count(&count).Error; err != nil {
		return err
	}
	if count > 1 {
		// reorder sort
		if err := reorderSortWithNode(tx); err != nil {
			logger.Errorf("[Server] BeforeUpdate reorderSort error: %v", err.Error())
			return err
		}
		// get max sort
		var maxSort int
		if err := tx.Model(&Server{}).Select("MAX(sort)").Scan(&maxSort).Error; err != nil {
			return err
		}
		n.Sort = maxSort + 1
	}
	return nil
}

func reorderSortWithNode(tx *gorm.DB) error {
	var nodes []Node
	if err := tx.Order("sort, id").Find(&nodes).Error; err != nil {
		return err
	}
	for i, node := range nodes {
		if node.Sort != i+1 {
			if err := tx.Exec("UPDATE `nodes` SET sort = ? WHERE id = ?", i+1, node.Id).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
