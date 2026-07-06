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
	ServerId int64 `json:"server_id"`
	Download int64 `json:"download"`
	Upload   int64 `json:"upload"`
	Total    int64 `json:"total"`
}

type UserTrafficRanking struct {
	UserId      int64 `json:"user_id"`
	SubscribeId int64 `json:"subscribe_id"`
	Download    int64 `json:"download"`
	Upload      int64 `json:"upload"`
	Total       int64 `json:"total"`
}
