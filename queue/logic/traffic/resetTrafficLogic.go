package traffic

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/queue/types"
	"go.uber.org/zap"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// ResetTrafficLogic handles traffic reset logic for different subscription cycles
// Supports three reset modes:
// - reset_cycle = 1: Reset on 1st of every month
// - reset_cycle = 2: Reset monthly based on subscription start date
// - reset_cycle = 3: Reset yearly based on subscription start date
type ResetTrafficLogic struct {
	svc *svc.ServiceContext
}

// Cache and retry configuration constants
const (
	maxRetryAttempts = 3
	retryDelay       = 30 * time.Minute
	lockTimeout      = 5 * time.Minute
)

// Cache keys
var (
	cacheKey      = "reset_traffic_cache"
	retryCountKey = "reset_traffic_retry_count"
	lockKey       = "reset_traffic_lock"
)

// resetTrafficCache stores the last reset time to prevent duplicate processing
type resetTrafficCache struct {
	LastResetTime time.Time
}

func NewResetTrafficLogic(svc *svc.ServiceContext) *ResetTrafficLogic {
	return &ResetTrafficLogic{
		svc: svc,
	}
}

// ProcessTask executes the traffic reset task for all subscription types with enhanced retry mechanism
func (l *ResetTrafficLogic) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	var err error
	startTime := time.Now()

	// Get current retry count
	retryCount := l.getRetryCount(ctx)
	zap.S().Infow("[ResetTraffic] Starting task execution",
		zap.Any("retryCount", retryCount),
		zap.Any("startTime", startTime))

	// Acquire distributed lock to prevent duplicate execution
	lockAcquired := l.acquireLock(ctx)
	if !lockAcquired {
		zap.S().Infow("[ResetTraffic] Another task is already running, skipping execution")
		return nil
	}
	defer l.releaseLock(ctx)

	defer func() {
		if err != nil {
			// Check if error is retryable and within retry limit
			if l.isRetryableError(err) && retryCount < maxRetryAttempts {
				// Increment retry count
				l.setRetryCount(ctx, retryCount+1)

				// Schedule retry with delay
				task := asynq.NewTask(types.SchedulerResetTraffic, nil)
				_, retryErr := l.svc.Queue.Enqueue(task, asynq.ProcessIn(retryDelay))
				if retryErr != nil {
					zap.S().Errorw("[ResetTraffic] Failed to enqueue retry task",
						zap.Any("error", retryErr.Error()),
						zap.Any("retryCount", retryCount))
				} else {
					zap.S().Infow("[ResetTraffic] Task failed, retrying in 30 minutes",
						zap.Any("error", err.Error()),
						zap.Any("retryCount", retryCount+1),
						zap.Any("maxRetryAttempts", maxRetryAttempts))
				}
			} else {
				// Max retries reached or non-retryable error
				if retryCount >= maxRetryAttempts {
					zap.S().Errorw("[ResetTraffic] Max retry attempts reached, giving up",
						zap.Any("retryCount", retryCount),
						zap.Any("maxRetryAttempts", maxRetryAttempts),
						zap.Any("error", err.Error()))
				} else {
					zap.S().Errorw("[ResetTraffic] Non-retryable error, not retrying",
						zap.Any("error", err.Error()),
						zap.Any("retryCount", retryCount))
				}
				// Reset retry count for next scheduled task
				l.clearRetryCount(ctx)
			}
		} else {
			// Task completed successfully, reset retry count
			l.clearRetryCount(ctx)
			zap.S().Infow("[ResetTraffic] Task completed successfully",
				zap.Any("processingTime", time.Since(startTime)),
				zap.Any("retryCount", retryCount))
		}
	}()

	// Load last reset time from cache
	var cache resetTrafficCache
	cacheData, err := l.svc.Redis.Get(ctx, cacheKey).Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			zap.S().Errorw("[ResetTraffic] Failed to get cache", zap.Any("error", err.Error()))
		}
		// Set default value if cache not found
		cache = resetTrafficCache{}
		zap.S().Infow("[ResetTraffic] Using default cache value", zap.Any("lastResetTime", cache.LastResetTime))
	} else {
		// Parse JSON data
		if err := json.Unmarshal([]byte(cacheData), &cache); err != nil {
			zap.S().Errorw("[ResetTraffic] Failed to unmarshal cache", zap.Any("error", err.Error()))
			cache = resetTrafficCache{}
		} else {
			zap.S().Infow("[ResetTraffic] Cache loaded successfully", zap.Any("lastResetTime", cache.LastResetTime))
		}
	}

	// Execute reset operations in order: yearly -> monthly (1st) -> monthly (cycle)
	err = l.resetYear(ctx)
	if err != nil {
		zap.S().Errorw("[ResetTraffic] Yearly reset failed", zap.Any("error", err.Error()))
		return err
	}

	err = l.reset1st(ctx, cache)
	if err != nil {
		zap.S().Errorw("[ResetTraffic] Monthly 1st reset failed", zap.Any("error", err.Error()))
		return err
	}

	err = l.resetMonth(ctx)
	if err != nil {
		zap.S().Errorw("[ResetTraffic] Monthly cycle reset failed", zap.Any("error", err.Error()))
		return err
	}

	// Update cache with current time after successful processing
	updatedCache := resetTrafficCache{
		LastResetTime: startTime,
	}
	cacheDataBytes, marshalErr := json.Marshal(updatedCache)
	if marshalErr != nil {
		zap.S().Errorw("[ResetTraffic] Failed to marshal cache", zap.Any("error", marshalErr.Error()))
	} else {
		cacheErr := l.svc.Redis.Set(ctx, cacheKey, cacheDataBytes, 0).Err()
		if cacheErr != nil {
			zap.S().Errorw("[ResetTraffic] Failed to update cache", zap.Any("error", cacheErr.Error()))
			// Don't return error here as the main task completed successfully
		} else {
			zap.S().Infow("[ResetTraffic] Cache updated successfully", zap.Any("newLastResetTime", startTime))
		}
	}

	return nil
}

// resetMonth handles monthly cycle reset based on subscription start date
// reset_cycle = 2: Reset monthly based on subscription start date
func (l *ResetTrafficLogic) resetMonth(ctx context.Context) error {
	now := time.Now()

	err := l.svc.Store.InTx(ctx, func(store repository.Store) error {
		// Get all subscriptions that reset monthly based on start date
		resetMonthSubIds, err := store.Subscribe().QueryResetCycleSubscribeIds(ctx, 2)
		if err != nil {
			zap.S().Errorw("[ResetTraffic] Failed to query monthly subscriptions", zap.Any("error", err.Error()))
			return err
		}

		if len(resetMonthSubIds) == 0 {
			zap.S().Infow("[ResetTraffic] No monthly cycle subscriptions found")
			return nil
		}

		// Query users for monthly reset based on subscription start date cycle
		monthlyResetUsers, err := store.User().QueryMonthlyResetSubscribeIds(ctx, resetMonthSubIds, now)
		if err != nil {
			zap.S().Errorw("[ResetTraffic] Failed to query monthly reset users", zap.Any("error", err.Error()))
			return err
		}

		if len(monthlyResetUsers) > 0 {
			zap.S().Infow("[ResetTraffic] Found users for monthly reset",
				zap.Any("count", len(monthlyResetUsers)),
				zap.Any("userIds", monthlyResetUsers))

			if err = store.User().ResetSubscribeTrafficByIds(ctx, monthlyResetUsers); err != nil {
				zap.S().Errorw("[ResetTraffic] Failed to update monthly reset users", zap.Any("error", err.Error()))
				return err
			}
			// Find user subscriptions for these users
			userSubs, err := store.User().FindSubscribesByIds(ctx, monthlyResetUsers)
			if err != nil {
				zap.S().Errorw("[ResetTraffic] Failed to find user subscriptions for 1st reset", zap.Any("error", err.Error()))
				return err
			}
			// Clear cache for these subscriptions
			l.clearCache(ctx, userSubs)
			zap.S().Infow("[ResetTraffic] Monthly reset completed", zap.Any("count", len(monthlyResetUsers)))
		} else {
			zap.S().Infow("[ResetTraffic] No users found for monthly reset")
		}
		return store.Subscribe().ClearCache(ctx, resetMonthSubIds...)
	})
	if err != nil {
		zap.S().Errorw("[ResetTraffic] Monthly reset transaction failed", zap.Any("error", err.Error()))
		return err
	}

	zap.S().Infow("[ResetTraffic] Monthly reset process completed")
	return nil
}

// reset1st handles reset on 1st of every month
// reset_cycle = 1: Reset on 1st of every month
func (l *ResetTrafficLogic) reset1st(ctx context.Context, cache resetTrafficCache) error {
	now := time.Now()

	// Check if we already reset this month using cache
	if firstDayResetAlreadyProcessed(now, cache) {
		zap.S().Infow("[ResetTraffic] Already reset this month, skipping 1st reset",
			zap.Any("lastResetTime", cache.LastResetTime),
			zap.Any("currentTime", now))
		return nil
	}

	// Only reset if it's the 1st day of the month
	if now.Day() != 1 {
		zap.S().Infow("[ResetTraffic] Not 1st day of month, skipping 1st reset", zap.Any("currentDay", now.Day()))
		return nil
	}

	err := l.svc.Store.InTx(ctx, func(store repository.Store) error {
		// Get all subscriptions that reset on 1st of month
		reset1stSubIds, err := store.Subscribe().QueryResetCycleSubscribeIds(ctx, 1)
		if err != nil {
			zap.S().Errorw("[ResetTraffic] Failed to query 1st reset subscriptions", zap.Any("error", err.Error()))
			return err
		}

		if len(reset1stSubIds) == 0 {
			zap.S().Infow("[ResetTraffic] No 1st reset subscriptions found")
			return nil
		}

		// Get all active users with these subscriptions
		users1stReset, err := store.User().QueryFirstResetSubscribeIds(ctx, reset1stSubIds, now)
		if err != nil {
			zap.S().Errorw("[ResetTraffic] Failed to query 1st reset users", zap.Any("error", err.Error()))
			return err
		}

		if len(users1stReset) > 0 {
			zap.S().Infow("[ResetTraffic] Found users for 1st reset",
				zap.Any("count", len(users1stReset)),
				zap.Any("userIds", users1stReset))

			// Reset upload and download traffic to zero
			if err = store.User().ResetSubscribeTrafficByIds(ctx, users1stReset); err != nil {
				zap.S().Errorw("[ResetTraffic] Failed to update 1st reset users", zap.Any("error", err.Error()))
				return err
			}
			userSubs, err := store.User().FindSubscribesByIds(ctx, users1stReset)
			if err != nil {
				zap.S().Errorw("[ResetTraffic] Failed to find user subscriptions for 1st reset", zap.Any("error", err.Error()))
				return err
			}

			// Clear cache for these subscriptions
			l.clearCache(ctx, userSubs)
			zap.S().Infow("[ResetTraffic] 1st reset completed", zap.Any("count", len(users1stReset)))
		} else {
			zap.S().Infow("[ResetTraffic] No users found for 1st reset")
		}

		return store.Subscribe().ClearCache(ctx, reset1stSubIds...)
	})

	if err != nil {
		zap.S().Errorw("[ResetTraffic] 1st reset transaction failed", zap.Any("error", err.Error()))
		return err
	}
	zap.S().Infow("[ResetTraffic] 1st reset process completed")
	return nil
}

func firstDayResetAlreadyProcessed(now time.Time, cache resetTrafficCache) bool {
	return !cache.LastResetTime.IsZero() &&
		cache.LastResetTime.Year() == now.Year() &&
		cache.LastResetTime.Month() == now.Month()
}

// resetYear handles yearly reset based on subscription start date anniversary
// reset_cycle = 3: Reset yearly based on subscription start date
func (l *ResetTrafficLogic) resetYear(ctx context.Context) error {
	now := time.Now()

	err := l.svc.Store.InTx(ctx, func(store repository.Store) error {
		// Get all subscriptions that reset yearly
		resetYearSubIds, err := store.Subscribe().QueryResetCycleSubscribeIds(ctx, 3)
		if err != nil {
			zap.S().Errorw("[ResetTraffic] Failed to query yearly subscriptions", zap.Any("error", err.Error()))
			return err
		}

		if len(resetYearSubIds) == 0 {
			zap.S().Infow("[ResetTraffic] No yearly reset subscriptions found")
			return nil
		}

		// Query users for yearly reset based on subscription start date anniversary
		usersYearReset, err := store.User().QueryYearlyResetSubscribeIds(ctx, resetYearSubIds, now)
		if err != nil {
			zap.S().Errorw("[ResetTraffic] Query yearly reset users failed", zap.Any("error", err.Error()))
			return err
		}

		if len(usersYearReset) > 0 {
			zap.S().Infow("[ResetTraffic] Found users for yearly reset",
				zap.Any("count", len(usersYearReset)),
				zap.Any("userIds", usersYearReset))

			// Reset upload and download traffic to zero
			if err = store.User().ResetSubscribeTrafficByIds(ctx, usersYearReset); err != nil {
				zap.S().Errorw("[ResetTraffic] Failed to update yearly reset users", zap.Any("error", err.Error()))
				return err
			}
			// Find user subscriptions for these users
			userSubs, err := store.User().FindSubscribesByIds(ctx, usersYearReset)
			if err != nil {
				zap.S().Errorw("[ResetTraffic] Failed to find user subscriptions for 1st reset", zap.Any("error", err.Error()))
				return err
			}
			// Clear cache for these subscriptions
			l.clearCache(ctx, userSubs)
			zap.S().Infow("[ResetTraffic] Yearly reset completed", zap.Any("count", len(usersYearReset)))
		} else {
			zap.S().Infow("[ResetTraffic] No users found for yearly reset")
		}
		err = store.Subscribe().ClearCache(ctx, resetYearSubIds...)
		if err != nil {
			zap.S().Errorw("[ResetTraffic] Failed to clear yearly reset subscription cache", zap.Any("error", err.Error()))
		}
		return nil
	})

	if err != nil {
		zap.S().Errorw("[ResetTraffic] Yearly reset transaction failed", zap.Any("error", err.Error()))
		return err
	}

	zap.S().Infow("[ResetTraffic] Yearly reset process completed")
	return nil
}

// getRetryCount retrieves the current retry count from Redis
func (l *ResetTrafficLogic) getRetryCount(ctx context.Context) int {
	countStr, err := l.svc.Redis.Get(ctx, retryCountKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0 // No retry count found, start with 0
		}
		zap.S().Errorw("[ResetTraffic] Failed to get retry count", zap.Any("error", err.Error()))
		return 0
	}

	count, err := strconv.Atoi(countStr)
	if err != nil {
		zap.S().Errorw("[ResetTraffic] Invalid retry count format", zap.Any("value", countStr))
		return 0
	}

	return count
}

// setRetryCount sets the retry count in Redis
func (l *ResetTrafficLogic) setRetryCount(ctx context.Context, count int) {
	err := l.svc.Redis.Set(ctx, retryCountKey, count, 24*time.Hour).Err()
	if err != nil {
		zap.S().Errorw("[ResetTraffic] Failed to set retry count",
			zap.Any("count", count),
			zap.Any("error", err.Error()))
	}
}

// clearRetryCount removes the retry count from Redis
func (l *ResetTrafficLogic) clearRetryCount(ctx context.Context) {
	err := l.svc.Redis.Del(ctx, retryCountKey).Err()
	if err != nil {
		zap.S().Errorw("[ResetTraffic] Failed to clear retry count", zap.Any("error", err.Error()))
	}
}

// acquireLock attempts to acquire a distributed lock
func (l *ResetTrafficLogic) acquireLock(ctx context.Context) bool {
	result := l.svc.Redis.SetNX(ctx, lockKey, "locked", lockTimeout)
	acquired, err := result.Result()
	if err != nil {
		zap.S().Errorw("[ResetTraffic] Failed to acquire lock", zap.Any("error", err.Error()))
		return false
	}

	if acquired {
		zap.S().Infow("[ResetTraffic] Lock acquired successfully")
	} else {
		zap.S().Infow("[ResetTraffic] Lock already exists, another task is running")
	}

	return acquired
}

// releaseLock releases the distributed lock
func (l *ResetTrafficLogic) releaseLock(ctx context.Context) {
	err := l.svc.Redis.Del(ctx, lockKey).Err()
	if err != nil {
		zap.S().Errorw("[ResetTraffic] Failed to release lock", zap.Any("error", err.Error()))
	} else {
		zap.S().Infow("[ResetTraffic] Lock released successfully")
	}
}

// isRetryableError determines if an error is retryable
func (l *ResetTrafficLogic) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errorMessage := strings.ToLower(err.Error())

	// Network and connection errors (retryable)
	retryableErrors := []string{
		"connection refused",
		"connection reset",
		"connection timeout",
		"network",
		"timeout",
		"dial",
		"context deadline exceeded",
		"temporary failure",
		"server error",
		"service unavailable",
		"internal server error",
		"database is locked",
		"too many connections",
		"deadlock",
		"lock wait timeout",
	}

	// Database constraint errors (non-retryable)
	nonRetryableErrors := []string{
		"foreign key constraint",
		"unique constraint",
		"check constraint",
		"not null constraint",
		"invalid input syntax",
		"column does not exist",
		"table does not exist",
		"permission denied",
		"access denied",
		"authentication failed",
		"invalid credentials",
	}

	// Check for non-retryable errors first
	for _, nonRetryable := range nonRetryableErrors {
		if strings.Contains(errorMessage, nonRetryable) {
			zap.S().Infow("[ResetTraffic] Non-retryable error detected",
				zap.Any("error", err.Error()),
				zap.Any("pattern", nonRetryable))
			return false
		}
	}

	// Check for retryable errors
	for _, retryable := range retryableErrors {
		if strings.Contains(errorMessage, retryable) {
			zap.S().Infow("[ResetTraffic] Retryable error detected",
				zap.Any("error", err.Error()),
				zap.Any("pattern", retryable))
			return true
		}
	}

	// Default: treat unknown errors as retryable, but log for analysis
	zap.S().Infow("[ResetTraffic] Unknown error type, treating as retryable",
		zap.Any("error", err.Error()))
	return true
}

// clearCache clears the reset traffic cache
// Uses an independent background context with a per-item timeout so that a
// long-running parent context deadline (e.g. asynq task timeout) does not
// cause cache/log operations to fail mid-way through large batches.
func (l *ResetTrafficLogic) clearCache(_ context.Context, list []*user.Subscribe) {
	if len(list) != 0 {
		subs := make(map[int64]bool)

		for _, sub := range list {
			if sub.SubscribeId > 0 {
				if _, ok := subs[sub.SubscribeId]; !ok {
					subs[sub.SubscribeId] = true
				}
			}
			// Insert traffic reset log
			l.insertLog(sub.Id, sub.UserId)
		}

		for sub := range subs {
			subCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := l.svc.Store.Subscribe().ClearCache(subCtx, sub); err != nil {
				zap.S().Errorw("[ResetTraffic] Failed to clear subscription cache",
					zap.Any("subscribeId", sub),
					zap.Any("error", err.Error()),
				)
			}
			cancel()
		}
	}
}

// insertLog inserts a reset traffic log entry using an independent background
// context so that asynq task deadline does not cancel log writes mid-batch.
func (l *ResetTrafficLogic) insertLog(subId, userId int64) {
	trafficLog := log.ResetSubscribe{
		Type:      log.ResetSubscribeTypeAuto,
		UserId:    userId,
		Timestamp: time.Now().UnixMilli(),
	}
	content, _ := trafficLog.Marshal()
	logCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := l.svc.Store.Log().Insert(logCtx, &log.SystemLog{
		Type:     log.TypeResetSubscribe.Uint8(),
		ObjectID: subId,
		Date:     time.Now().Format(time.DateOnly),
		Content:  string(content),
	}); err != nil {
		zap.S().Errorw("[ResetTraffic] Failed to create system log for subscription", zap.Any("error", err.Error()))
	}
}
