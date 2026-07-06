package subscribe

import (
	"database/sql/driver"
	"encoding/json"
	"time"
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
	Id                int64
	Name              string
	Language          string
	Description       string
	UnitPrice         int64
	UnitTime          string
	Discount          string
	Replacement       int64
	Inventory         int64
	Traffic           int64
	TrafficUnlimited  bool
	SpeedLimit        int64
	DeviceLimit       int64
	Quota             int64
	Nodes             string
	NodeTags          string
	NodeGroupIds      JSONInt64Slice
	NodeGroupId       int64
	TrafficLimit      string
	Show              *bool
	Sell              *bool
	Sort              int64
	DeductionRatio    int64
	AllowDeduction    *bool
	ResetCycle        int64
	RenewalReset      *bool
	ShowOriginalPrice bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (*Subscribe) TableName() string {
	return "subscribe"
}

type Discount struct {
	Months   int64 `json:"months"`
	Discount int64 `json:"discount"`
}

type Group struct {
	Id          int64
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (Group) TableName() string {
	return "subscribe_group"
}
