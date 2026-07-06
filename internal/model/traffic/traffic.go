package traffic

import "time"

//goland:noinspection GoNameStartsWithPackageName
type TrafficLog struct {
	Id          int64
	ServerId    int64
	UserId      int64
	SubscribeId int64
	Download    int64
	Upload      int64
	Timestamp   time.Time
}

type TotalTraffic struct {
	Download int64
	Upload   int64
}

type ServerTrafficRanking struct {
	ServerId int64
	Download int64
	Upload   int64
	Total    int64
}

type UserTrafficRanking struct {
	UserId      int64
	SubscribeId int64
	Download    int64
	Upload      int64
	Total       int64
}

func (TrafficLog) TableName() string {
	return "traffic_log"
}
