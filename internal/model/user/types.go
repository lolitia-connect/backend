package user

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/perfect-panel/server/internal/model/subscribe"
)

// JSONInt64Slice is a custom type for handling []int64 as JSON in database
type JSONInt64Slice []int64

// Scan implements sql.Scanner interface
func (j *JSONInt64Slice) Scan(value interface{}) error {
	if value == nil {
		*j = []int64{}
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
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
	if bytes[0] != '[' {
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

type SubscribeDetails struct {
	Id               int64
	UserId           int64
	User             *User
	OrderId          int64
	SubscribeId      int64
	Subscribe        *subscribe.Subscribe
	NodeGroupId      int64
	GroupLocked      *bool
	StartTime        time.Time
	ExpireTime       time.Time
	FinishedAt       *time.Time
	Traffic          int64
	TrafficUnlimited bool
	Download         int64
	Upload           int64
	Token            string
	UUID             string
	Status           uint8
	Note             string
	CreatedAt        time.Time
	UpdatedAt        time.Time
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

type SubscribeLogFilterParams struct {
	IP              string
	UserAgent       string
	UserId          int64
	Token           string
	UserSubscribeId int64
}

type LoginLogFilterParams struct {
	IP        string
	UserId    int64
	UserAgent string
	Success   *bool
}

type UserFilterParams struct {
	Search          string
	UserId          *int64
	SubscribeId     *int64
	UserSubscribeId *int64
	ShortCode       string
	Order           string // Order by id, e.g., "desc"
	Unscoped        bool   // Whether to include soft-deleted records
}

type EmailRecipientFilter struct {
	Scope             int8
	RegisterStartTime int64
	RegisterEndTime   int64
}

type SubscribeFilter struct {
	Subscribers []int64
	IsActive    *bool
	StartTime   int64
	EndTime     int64
}

type UserStatisticsWithDate struct {
	Date              string
	Register          int64
	NewOrderUsers     int64
	RenewalOrderUsers int64
}
