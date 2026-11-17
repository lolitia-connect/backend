package types

import "time"

const BatchEmailTaskTimeout = 6 * time.Hour

const (
	// ScheduledBatchSendEmail scheduled batch send email
	ScheduledBatchSendEmail = "scheduled:email:batch"

	// ForthwithQuotaTask create quota task immediately
	ForthwithQuotaTask = "forthwith:quota:task"

	// SchedulerExchangeRate fetch exchange rate task
	SchedulerExchangeRate = "scheduler:exchange:rate"
)
