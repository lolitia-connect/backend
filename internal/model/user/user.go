package user

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

type User struct {
	Id                    int64
	Password              string
	Algo                  string
	Salt                  string
	Avatar                string
	Balance               int64 // User Balance Amount
	ReferCode             string
	RefererId             int64
	Commission            int64 // Commission Amount
	ReferralPercentage    uint8 // Referral Percentage
	OnlyFirstPurchase     *bool // Only First Purchase Referral
	GiftAmount            int64
	Enable                *bool
	IsAdmin               *bool
	EnableBalanceNotify   *bool
	EnableLoginNotify     *bool
	EnableSubscribeNotify *bool
	EnableTradeNotify     *bool
	AuthMethods           []AuthMethods
	UserDevices           []Device
	Rules                 string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	DeletedAt             *time.Time
}

func (*User) TableName() string {
	return "user"
}

type Subscribe struct {
	Id               int64
	UserId           int64
	User             User
	OrderId          int64
	SubscribeId      int64
	NodeGroupId      int64
	GroupLocked      *bool
	StartTime        time.Time
	ExpireTime       time.Time
	FinishedAt       *time.Time
	Traffic          int64
	TrafficUnlimited bool
	Download         int64
	Upload           int64
	ExpiredDownload  int64
	ExpiredUpload    int64
	Token            string
	UUID             string
	Status           uint8
	Note             string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (*Subscribe) TableName() string {
	return "user_subscribe"
}

type AuthMethods struct {
	Id             int64
	UserId         int64
	AuthType       string
	AuthIdentifier string
	Verified       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (*AuthMethods) TableName() string {
	return "user_auth_methods"
}

type Device struct {
	Id         int64
	Ip         string
	UserId     int64
	UserAgent  string
	Identifier string
	ShortCode  string
	Online     bool
	Enabled    bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (*Device) TableName() string {
	return "user_device"
}

type DeviceOnlineRecord struct {
	Id            int64
	UserId        int64
	Identifier    string
	OnlineTime    time.Time // The time when the device goes online
	OfflineTime   time.Time
	OnlineSeconds int64
	DurationDays  int64
	CreatedAt     time.Time
}

func (DeviceOnlineRecord) TableName() string {
	return "user_device_online_record"
}

type Withdrawal struct {
	Id        int64
	UserId    int64
	Amount    int64
	Content   string
	Status    uint8
	Reason    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (*Withdrawal) TableName() string {
	return "user_withdrawal"
}
