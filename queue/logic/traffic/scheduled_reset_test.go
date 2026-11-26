package traffic

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/queue/types"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestScheduledTrafficResetRetriesOnlyAfterRollingBack(t *testing.T) {
	service, inspector := trafficStatTestService(t)
	ctx := t.Context()
	now := time.Now()
	plan := service.Ent.Subscribe.Create().SetResetCycle(3).SaveX(ctx)
	start := now.AddDate(-2, 0, -1)
	subscription := service.Ent.UserSubscribe.Create().SetUserID(31).SetOrderID(1).SetSubscribeID(plan.ID).
		SetStartTime(start).SetExpireTime(now.AddDate(0, 1, 0)).SetTrafficResetAt(start).
		SetStatus(2).SetUpload(17).SetDownload(35).SaveX(ctx)

	failure := errors.New("database is locked")
	service.Ent.SystemLog.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if failure != nil && mutation.Op().Is(ent.OpCreate) {
				return nil, failure
			}
			return next.Mutate(ctx, mutation)
		})
	})
	logic := NewResetTrafficLogic(service)
	require.ErrorIs(t, logic.ProcessTask(ctx, nil), failure)
	unchanged := service.Ent.UserSubscribe.GetX(ctx, subscription.ID)
	require.Equal(t, subscription.Upload, unchanged.Upload)
	require.Equal(t, subscription.Download, unchanged.Download)
	require.Equal(t, subscription.Status, unchanged.Status)
	require.Zero(t, service.Ent.SystemLog.Query().CountX(ctx))
	require.ErrorIs(t, service.Redis.Get(ctx, cacheKey).Err(), redis.Nil)
	require.Equal(t, "1", service.Redis.Get(ctx, retryCountKey).Val())

	retries, err := inspector.ListScheduledTasks("default")
	require.NoError(t, err)
	require.Len(t, retries, 1)
	require.Equal(t, types.SchedulerResetTraffic, retries[0].Type)
	require.WithinDuration(t, now.Add(30*time.Minute), retries[0].NextProcessAt, time.Minute)

	failure = nil
	require.NoError(t, logic.ProcessTask(ctx, nil))
	reset := service.Ent.UserSubscribe.GetX(ctx, subscription.ID)
	require.Zero(t, reset.Upload)
	require.Zero(t, reset.Download)
	require.Equal(t, uint8(1), reset.Status)
	require.Equal(t, subscription.ID, service.Ent.SystemLog.Query().OnlyX(ctx).ObjectID)
	require.ErrorIs(t, service.Redis.Get(ctx, retryCountKey).Err(), redis.Nil)
	var checkpoint resetTrafficCache
	data, err := service.Redis.Get(ctx, cacheKey).Bytes()
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &checkpoint))
	require.WithinDuration(t, now, checkpoint.LastResetTime, time.Minute)
}

func TestScheduledResetResumesAcrossCyclesWithoutLosingNewTraffic(t *testing.T) {
	service, _ := trafficStatTestService(t)
	ctx := t.Context()
	db := service.Ent
	now := time.Now()
	seed := func(cycle, account int64) *ent.UserSubscribe {
		plan := db.Subscribe.Create().SetResetCycle(cycle).SaveX(ctx)
		start := now.AddDate(-3, 0, -2)
		return db.UserSubscribe.Create().SetUserID(account).SetOrderID(account).SetSubscribeID(plan.ID).
			SetStartTime(start).SetTrafficResetAt(start).SetStatus(2).SetUpload(20).SetDownload(30).SaveX(ctx)
	}
	yearly, first, monthly := seed(3, 31), seed(1, 32), seed(2, 33)
	unavailable := errors.New("database is locked")
	blocked := true
	db.SystemLog.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			entry := mutation.(*ent.SystemLogMutation)
			if id, _ := entry.ObjectID(); blocked && id == first.ID {
				return nil, unavailable
			}
			return next.Mutate(ctx, mutation)
		})
	})
	worker := NewResetTrafficLogic(service)
	require.ErrorIs(t, worker.ProcessTask(ctx, nil), unavailable)
	require.Equal(t, yearly.ID, db.SystemLog.Query().OnlyX(ctx).ObjectID)
	require.Zero(t, db.UserSubscribe.GetX(ctx, yearly.ID).Download)
	for _, item := range []*ent.UserSubscribe{first, monthly} {
		current := db.UserSubscribe.GetX(ctx, item.ID)
		require.Equal(t, item.Download, current.Download)
		require.Equal(t, item.Status, current.Status)
	}
	db.UserSubscribe.UpdateOneID(yearly.ID).SetDownload(95).ExecX(ctx)
	blocked = false
	require.NoError(t, worker.ProcessTask(ctx, nil))
	require.NoError(t, worker.ProcessTask(ctx, nil))
	require.EqualValues(t, 95, db.UserSubscribe.GetX(ctx, yearly.ID).Download)
	for _, item := range []*ent.UserSubscribe{first, monthly} {
		current := db.UserSubscribe.GetX(ctx, item.ID)
		require.Zero(t, current.Upload)
		require.Zero(t, current.Download)
		require.EqualValues(t, 1, current.Status)
	}
	entries := db.SystemLog.Query().AllX(ctx)
	var resetIDs []int64
	for _, entry := range entries {
		resetIDs = append(resetIDs, entry.ObjectID)
	}
	require.ElementsMatch(t, []int64{yearly.ID, first.ID, monthly.ID}, resetIDs)
}

func TestMonthlyTrafficResetCatchesUpOnceAndPreservesIneligibleUsage(t *testing.T) {
	service, _ := trafficStatTestService(t)
	ctx := t.Context()
	db := service.Ent
	now := time.Date(2026, time.April, 3, 12, 0, 0, 0, time.UTC)
	start := time.Date(2026, time.January, 31, 12, 0, 0, 0, time.UTC)
	marker := time.Date(2026, time.February, 28, 0, 0, 0, 0, time.UTC)
	monthly := db.Subscribe.Create().SetResetCycle(2).SaveX(ctx)
	otherCycle := db.Subscribe.Create().SetResetCycle(3).SaveX(ctx)
	seed := func() *ent.UserSubscribeCreate {
		return db.UserSubscribe.Create().SetUserID(31).SetOrderID(1).SetSubscribeID(monthly.ID).
			SetStartTime(start).SetExpireTime(now.AddDate(0, 1, 0)).SetTrafficResetAt(marker).
			SetStatus(2).SetFinishedAt(now.Add(-time.Hour)).SetUpload(17).SetDownload(35)
	}
	eligible := seed().SaveX(ctx)
	providerManaged := seed().SetEntitlementSource("apple").SaveX(ctx)
	expired := seed().SetExpireTime(now.Add(-time.Hour)).SaveX(ctx)
	yearly := seed().SetSubscribeID(otherCycle.ID).SaveX(ctx)

	logic := NewResetTrafficLogic(service)
	require.NoError(t, logic.resetCycle(ctx, 2, now))
	reset := db.UserSubscribe.GetX(ctx, eligible.ID)
	require.Zero(t, reset.Upload)
	require.Zero(t, reset.Download)
	require.Equal(t, uint8(1), reset.Status)
	require.Nil(t, reset.FinishedAt)
	require.NotNil(t, reset.TrafficResetAt)
	day := time.Date(2026, time.April, 3, 0, 0, 0, 0, time.UTC)
	require.True(t, reset.TrafficResetAt.Equal(day), "reset marker = %s, want %s", reset.TrafficResetAt, day)

	// Traffic accumulated after catch-up must survive the next scheduled run.
	db.UserSubscribe.UpdateOneID(eligible.ID).SetUpload(5).SetDownload(11).ExecX(ctx)
	require.NoError(t, logic.resetCycle(ctx, 2, now.AddDate(0, 0, 1)))
	replayed := db.UserSubscribe.GetX(ctx, eligible.ID)
	require.Equal(t, int64(5), replayed.Upload)
	require.Equal(t, int64(11), replayed.Download)
	for _, original := range []*ent.UserSubscribe{providerManaged, expired, yearly} {
		got := db.UserSubscribe.GetX(ctx, original.ID)
		require.Equal(t, original.Upload, got.Upload, "subscription %d upload", got.ID)
		require.Equal(t, original.Download, got.Download, "subscription %d download", got.ID)
		require.Equal(t, original.Status, got.Status, "subscription %d status", got.ID)
	}

	entry := db.SystemLog.Query().OnlyX(ctx)
	require.Equal(t, log.TypeResetSubscribe.Uint8(), entry.Type)
	require.Equal(t, eligible.ID, entry.ObjectID)
	require.Equal(t, day.Format(time.DateOnly), entry.Date)
	var audit log.ResetSubscribe
	require.NoError(t, json.Unmarshal([]byte(entry.Content), &audit))
	require.Equal(t, log.ResetSubscribeTypeAuto, audit.Type)
	require.Equal(t, eligible.UserID, audit.UserId)
}
