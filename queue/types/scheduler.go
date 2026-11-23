package types

import "time"

type TrafficLogCleanup struct {
	Cutoff time.Time `json:"cutoff"`
}

const (
	CleanupTrafficLogs         = "traffic:cleanup:logs"
	SchedulerCheckSubscription = "scheduler:check:subscription"
	SchedulerTotalServerData   = "scheduler:total:server"
	SchedulerResetTraffic      = "scheduler:reset:traffic"
	SchedulerTrafficStat       = "scheduler:traffic:stat"
	SchedulerRecalculateGroup  = "scheduler:recalculate:group"
)
