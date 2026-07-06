package node

import "time"

type ServerConfigOverride struct {
	Id         int64
	ServerId   int64
	IPStrategy *string
	DNS        *string
	Block      *string
	Outbound   *string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
