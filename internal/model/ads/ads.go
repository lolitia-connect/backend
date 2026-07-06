package ads

import "time"

type Ads struct {
	Id          int64
	Title       string
	Type        string
	Content     string
	Description string
	TargetURL   string
	StartTime   time.Time
	EndTime     time.Time
	Status      int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (Ads) TableName() string {
	return "ads"
}
