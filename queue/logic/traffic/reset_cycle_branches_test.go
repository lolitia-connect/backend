package traffic

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScheduledResetPreservesUsageAlreadyInTheCurrentBillingMonth(t *testing.T) {
	service, _ := trafficStatTestService(t)
	ctx := t.Context()
	now := time.Now()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	plan := service.Ent.Subscribe.Create().SetResetCycle(1).SaveX(ctx)
	seed := func(userID int64, marker time.Time) int64 {
		return service.Ent.UserSubscribe.Create().SetUserID(userID).SetOrderID(userID).SetSubscribeID(plan.ID).
			SetStartTime(month.AddDate(0, -3, 0)).SetStatus(2).SetTrafficResetAt(marker).
			SetUpload(11).SetDownload(22).SaveX(ctx).ID
	}
	due := seed(31, month.AddDate(0, -1, 0))
	alreadyReset := seed(32, month)
	require.NoError(t, NewResetTrafficLogic(service).ProcessTask(ctx, nil))
	reset := service.Ent.UserSubscribe.GetX(ctx, due)
	require.Zero(t, reset.Upload)
	require.Zero(t, reset.Download)
	require.EqualValues(t, 1, reset.Status)
	preserved := service.Ent.UserSubscribe.GetX(ctx, alreadyReset)
	require.EqualValues(t, 11, preserved.Upload)
	require.EqualValues(t, 22, preserved.Download)
	require.EqualValues(t, 2, preserved.Status)
	require.Equal(t, due, service.Ent.SystemLog.Query().OnlyX(ctx).ObjectID)
}
