package subscribe

import (
	"database/sql/driver"
	"encoding/json"
	"time"

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

type Subscribe struct {
	Id                int64          `gorm:"primaryKey"`
	Name              string         `gorm:"type:varchar(255);not null;default:'';comment:Subscribe Name"`
	Language          string         `gorm:"type:varchar(255);not null;default:'';comment:Language"`
	Description       string         `gorm:"type:text;comment:Subscribe Description"`
	UnitPrice         int64          `gorm:"type:int;not null;default:0;comment:Unit Price"`
	UnitTime          string         `gorm:"type:varchar(255);not null;default:'';comment:Unit Time"`
	Discount          string         `gorm:"type:text;comment:Discount"`
	Replacement       int64          `gorm:"type:int;not null;default:0;comment:Replacement"`
	Inventory         int64          `gorm:"type:int;not null;default:-1;comment:Inventory"`
	Traffic           int64          `gorm:"type:int;not null;default:0;comment:Traffic"`
	SpeedLimit        int64          `gorm:"type:int;not null;default:0;comment:Speed Limit"`
	DeviceLimit       int64          `gorm:"type:int;not null;default:0;comment:Device Limit"`
	Quota             int64          `gorm:"type:int;not null;default:0;comment:Quota"`
	Nodes             string         `gorm:"type:varchar(255);comment:Node Ids"`
	NodeTags          string         `gorm:"type:varchar(255);comment:Node Tags"`
	NodeGroupIds      JSONInt64Slice `gorm:"type:json;comment:Node Group IDs (JSON array, multiple groups)"`
	NodeGroupId       int64          `gorm:"default:0;index:idx_node_group_id;comment:Default Node Group ID (single ID)"`
	Show              *bool          `gorm:"type:tinyint(1);not null;default:0;comment:Show portal page"`
	Sell              *bool          `gorm:"type:tinyint(1);not null;default:0;comment:Sell"`
	Sort              int64          `gorm:"type:int;not null;default:0;comment:Sort"`
	DeductionRatio    int64          `gorm:"type:int;default:0;comment:Deduction Ratio"`
	AllowDeduction    *bool          `gorm:"type:tinyint(1);default:1;comment:Allow deduction"`
	ResetCycle        int64          `gorm:"type:int;default:0;comment:Reset Cycle: 0: No Reset, 1: 1st, 2: Monthly, 3: Yearly"`
	RenewalReset      *bool          `gorm:"type:tinyint(1);default:0;comment:Renew Reset"`
	ShowOriginalPrice bool           `gorm:"type:tinyint(1);not null;default:1;comment:Show Original Price"`
	CreatedAt         time.Time      `gorm:"<-:create;comment:Create Time"`
	UpdatedAt         time.Time      `gorm:"comment:Update Time"`
}

func (*Subscribe) TableName() string {
	return "subscribe"
}

func (s *Subscribe) BeforeCreate(tx *gorm.DB) error {
	if s.Sort == 0 {
		var maxSort int64
		if err := tx.Model(&Subscribe{}).Select("COALESCE(MAX(sort), 0)").Scan(&maxSort).Error; err != nil {
			return err
		}
		s.Sort = maxSort + 1
	}
	return nil
}

func (s *Subscribe) BeforeDelete(tx *gorm.DB) error {
	if err := tx.Exec("UPDATE `subscribe` SET sort = sort - 1 WHERE sort > ?", s.Sort).Error; err != nil {
		return err
	}
	return nil
}
func (s *Subscribe) BeforeUpdate(tx *gorm.DB) error {
	var count int64
	if err := tx.Set("gorm:query_option", "FOR UPDATE").Model(&Subscribe{}).
		Where("sort = ? AND id != ?", s.Sort, s.Id).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		var maxSort int64
		if err := tx.Model(&Subscribe{}).Select("MAX(sort)").Scan(&maxSort).Error; err != nil {
			return err
		}
		s.Sort = maxSort + 1
	}
	return nil
}

type Discount struct {
	Months   int64 `json:"months"`
	Discount int64 `json:"discount"`
}

type Group struct {
	Id          int64     `gorm:"primaryKey"`
	Name        string    `gorm:"type:varchar(255);not null;default:'';comment:Group Name"`
	Description string    `gorm:"type:text;comment:Group Description"`
	CreatedAt   time.Time `gorm:"<-:create;comment:Create Time"`
	UpdatedAt   time.Time `gorm:"comment:Update Time"`
}

func (Group) TableName() string {
	return "subscribe_group"
}
