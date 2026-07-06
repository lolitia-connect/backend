package redemption

import "time"

type RedemptionCode struct {
	Id            int64
	Code          string
	TotalCount    int64
	UsedCount     int64
	SubscribePlan int64
	UnitTime      string
	Quantity      int64
	Status        int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time
}

type RedemptionRecord struct {
	Id               int64
	RedemptionCodeId int64
	UserId           int64
	SubscribeId      int64
	UnitTime         string
	Quantity         int64
	RedeemedAt       time.Time
	CreatedAt        time.Time
}

func (RedemptionCode) TableName() string {
	return "redemption_code"
}

func (RedemptionRecord) TableName() string {
	return "redemption_record"
}
