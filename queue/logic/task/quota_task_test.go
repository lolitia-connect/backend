package task

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/systemlog"
	modellog "github.com/perfect-panel/server/internal/model/log"
	modeltask "github.com/perfect-panel/server/internal/model/task"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/queue/types"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestQuotaTaskPersistsPartialGrantAndIgnoresRedelivery(t *testing.T) {
	service, db := quotaFixture(t)
	ctx := t.Context()
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	service.Store = repository.NewStore(db, cache)
	plan := db.Subscribe.Create().SetID(9).SetUnitPrice(1250).SaveX(ctx)
	activeID, terminalID, unselectedID := int64(41), int64(42), int64(43)
	seedSubscription(t, db, activeID, nil)
	seedSubscription(t, db, terminalID, func(create *ent.UserSubscribeCreate) { create.SetStatus(4) })
	seedSubscription(t, db, unselectedID, nil)
	active := db.UserSubscribe.GetX(ctx, activeID)
	terminal := db.UserSubscribe.GetX(ctx, terminalID)
	unselected := db.UserSubscribe.GetX(ctx, unselectedID)
	db.User.Create().SetID(active.UserID).SetPassword("fixture").SaveX(ctx)
	wallet := db.UserWallet.Create().SetUserID(active.UserID).SetBalance(400).SetGiftAmount(75).SaveX(ctx)

	scope := modeltask.QuotaScope{Subscribers: []int64{plan.ID}, Objects: []int64{activeID, terminalID}}
	content := modeltask.QuotaContent{Days: 3, ResetTraffic: true, GiftType: 2, GiftValue: 20}
	scopeJSON, err := scope.Marshal()
	require.NoError(t, err)
	contentJSON, err := content.Marshal()
	require.NoError(t, err)
	storedTask := db.Task.Create().SetType(int8(modeltask.TypeQuota)).SetScope(string(scopeJSON)).
		SetContent(string(contentJSON)).SetTotal(2).SaveX(ctx)
	logic := NewQuotaTaskLogic(service)
	job := asynq.NewTask(types.ForthwithQuotaTask, []byte(strconv.FormatInt(storedTask.ID, 10)))
	require.NoError(t, logic.ProcessTask(ctx, job))
	require.NoError(t, logic.ProcessTask(ctx, job))

	result := db.Task.GetX(ctx, storedTask.ID)
	require.Equal(t, int8(2), result.Status)
	require.Equal(t, uint64(2), result.Current)
	var failures []ErrorInfo
	require.NoError(t, json.Unmarshal([]byte(result.Errors), &failures))
	require.Len(t, failures, 1)
	require.Equal(t, terminalID, failures[0].UserSubscribeId)

	granted := db.UserSubscribe.GetX(ctx, activeID)
	wantExpiry := active.ExpireTime.AddDate(0, 0, 3)
	require.True(t, granted.ExpireTime.Equal(wantExpiry), "expiry = %s, want %s", granted.ExpireTime, wantExpiry)
	require.Zero(t, granted.Upload)
	require.Zero(t, granted.Download)
	for _, original := range []*ent.UserSubscribe{terminal, unselected} {
		got := db.UserSubscribe.GetX(ctx, original.ID)
		require.Equal(t, original.Status, got.Status, "subscription %d status", got.ID)
		require.True(t, got.ExpireTime.Equal(original.ExpireTime), "subscription %d expiry changed", got.ID)
		require.Equal(t, original.Upload, got.Upload, "subscription %d upload", got.ID)
		require.Equal(t, original.Download, got.Download, "subscription %d download", got.ID)
	}
	credited := db.UserWallet.GetX(ctx, wallet.ID)
	require.Equal(t, wallet.Balance, credited.Balance)
	require.Equal(t, int64(325), credited.GiftAmount)

	giftEntry := db.SystemLog.Query().Where(systemlog.TypeEQ(modellog.TypeGift.Uint8())).OnlyX(ctx)
	require.Equal(t, active.UserID, giftEntry.ObjectID)
	var gift modellog.Gift
	require.NoError(t, json.Unmarshal([]byte(giftEntry.Content), &gift))
	require.Equal(t, modellog.GiftTypeIncrease, gift.Type)
	require.Equal(t, activeID, gift.SubscribeId)
	require.Equal(t, int64(250), gift.Amount)
	require.Equal(t, credited.GiftAmount, gift.Balance)

	resetEntry := db.SystemLog.Query().Where(systemlog.TypeEQ(modellog.TypeResetSubscribe.Uint8())).OnlyX(ctx)
	require.Equal(t, activeID, resetEntry.ObjectID)
}

func TestQuotaTaskRejectsMalformedStoredContentWithoutClaiming(t *testing.T) {
	service, db := quotaFixture(t)
	ctx := t.Context()
	seedSubscription(t, db, 41, nil)
	before := db.UserSubscribe.GetX(ctx, 41)
	storedTask := db.Task.Create().SetType(int8(modeltask.TypeQuota)).SetScope(`{"recipients":[41]}`).
		SetContent(`{"reset_traffic":true,"days":"invalid"}`).SetTotal(1).SaveX(ctx)
	job := asynq.NewTask(types.ForthwithQuotaTask, []byte(strconv.FormatInt(storedTask.ID, 10)))

	require.ErrorIs(t, NewQuotaTaskLogic(service).ProcessTask(ctx, job), asynq.SkipRetry)
	result := db.Task.GetX(ctx, storedTask.ID)
	require.Zero(t, result.Status)
	require.Zero(t, result.Current)
	after := db.UserSubscribe.GetX(ctx, before.ID)
	require.Equal(t, before.Upload, after.Upload)
	require.Equal(t, before.Download, after.Download)
	require.Zero(t, db.SystemLog.Query().CountX(ctx))
}
