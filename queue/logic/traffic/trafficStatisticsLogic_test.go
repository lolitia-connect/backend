package traffic

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/trafficagg"
	paneltypes "github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/queue/types"
	"github.com/stretchr/testify/require"
)

func TestTrafficStatisticsSettlesScopedReportsOnce(t *testing.T) {
	service, _ := trafficStatTestService(t)
	ctx := t.Context()
	db := service.Ent
	server := db.Server.Create().SetProtocols(`[
		{"type":"vless","id":"primary","enable":true,"ratio":1},
		{"type":"vless","id":"metered","enable":true,"ratio":2}
	]`).SaveX(ctx)
	servedPlan := db.Subscribe.Create().SaveX(ctx)
	otherPlan := db.Subscribe.Create().SaveX(ctx)
	owner := db.User.Create().SetPassword("fixture").SaveX(ctx)
	served := db.UserSubscribe.Create().SetUserID(owner.ID).SetSubscribeID(servedPlan.ID).SetOrderID(1).
		SetStatus(1).SetUpload(10).SetDownload(20).SetUUID("32d0494a-24bc-44d0-a5e8-943f597b553c").SaveX(ctx)
	unserved := db.UserSubscribe.Create().SetUserID(owner.ID).SetSubscribeID(otherPlan.ID).SetOrderID(2).
		SetStatus(1).SetUpload(30).SetDownload(40).SetUUID("dd23cd2e-2af7-45f7-be17-d2ad2842288d").SaveX(ctx)

	// Seed the published user list so the queue test does not depend on MySQL routing queries.
	userList, err := json.Marshal(paneltypes.GetServerUserListResponse{Users: []paneltypes.ServerUser{
		{Id: served.ID, UUID: served.UUID},
	}})
	require.NoError(t, err)
	generation, err := node.ServerCacheGeneration(ctx, service.Redis, server.ID)
	require.NoError(t, err)
	key := fmt.Sprintf("%s%d:vless:metered", node.ServerUserListCacheKey, server.ID)
	published, err := node.PublishServerCache(ctx, service.Redis, server.ID, key, userList, generation)
	require.NoError(t, err)
	require.True(t, published)

	report := types.TrafficStatistics{ServerId: server.ID, Protocol: "vless", ProtocolId: "metered", Logs: []types.UserTraffic{
		{SID: served.ID, Upload: 7, Download: 9},
		{SID: unserved.ID, Upload: 100, Download: 200},
	}}
	payload, err := json.Marshal(report)
	require.NoError(t, err)
	logic := NewTrafficStatisticsLogic(service)
	job := asynq.NewTask(types.ForthwithTrafficStatistics, payload)
	require.NoError(t, logic.ProcessTask(ctx, job))
	require.NoError(t, logic.ProcessTask(ctx, job))

	report.Logs = []types.UserTraffic{{SID: served.ID, Upload: 3, Download: 1}}
	payload, err = json.Marshal(report)
	require.NoError(t, err)
	require.NoError(t, logic.ProcessTask(ctx, asynq.NewTask(types.ForthwithTrafficStatistics, payload)))
	require.Equal(t, 0, db.TrafficLog.Query().CountX(ctx))

	// Advance the settlement clock past all report buckets without waiting for a minute rollover.
	aggregator := &trafficagg.Aggregator{Store: service.Store, Redis: service.Redis}
	require.NoError(t, aggregator.Flush(ctx, time.Now().Add(time.Minute)))
	got := db.UserSubscribe.GetX(ctx, served.ID)
	require.Equal(t, int64(30), got.Upload)
	require.Equal(t, int64(40), got.Download)
	untouched := db.UserSubscribe.GetX(ctx, unserved.ID)
	require.Equal(t, unserved.Upload, untouched.Upload)
	require.Equal(t, unserved.Download, untouched.Download)

	var upload, download int64
	for _, entry := range db.TrafficLog.Query().AllX(ctx) {
		require.Equal(t, server.ID, entry.ServerID)
		require.Equal(t, owner.ID, entry.UserID)
		require.Equal(t, served.ID, entry.SubscribeID)
		upload += entry.Upload
		download += entry.Download
	}
	require.Equal(t, int64(20), upload)
	require.Equal(t, int64(20), download)
}

func TestFlushTrafficTaskPersistsClosedBucketAndHeartbeat(t *testing.T) {
	service, _ := trafficStatTestService(t)
	ctx := t.Context()
	db := service.Ent
	server := db.Server.Create().SetProtocols(`[{"type":"vless","id":"primary","enable":true}]`).SaveX(ctx)
	subscription := db.UserSubscribe.Create().SetUserID(17).SetSubscribeID(4).SetOrderID(1).SaveX(ctx)
	reportedAt := time.Now().UTC().Truncate(time.Minute).Add(-time.Minute)
	aggregator := &trafficagg.Aggregator{Store: service.Store, Redis: service.Redis}
	storedServer, err := service.Store.Node().FindOneServer(ctx, server.ID)
	require.NoError(t, err)
	require.NoError(t, aggregator.AddReportAt(ctx, storedServer, "vless", "primary", []trafficagg.Report{
		{SID: subscription.ID, Upload: 12, Download: 34},
	}, reportedAt))

	logic := NewFlushTrafficLogic(service)
	job := asynq.NewTask(types.SchedulerFlushTraffic, nil)
	require.NoError(t, logic.ProcessTask(ctx, job))
	require.NoError(t, logic.ProcessTask(ctx, job))

	got := db.UserSubscribe.GetX(ctx, subscription.ID)
	require.Equal(t, int64(12), got.Upload)
	require.Equal(t, int64(34), got.Download)
	entry := db.TrafficLog.Query().OnlyX(ctx)
	require.True(t, entry.Timestamp.Equal(reportedAt), "traffic log timestamp = %s, want %s", entry.Timestamp, reportedAt)
	lastReported := db.Server.GetX(ctx, server.ID).LastReportedAt
	require.NotNil(t, lastReported)
	require.True(t, lastReported.Equal(reportedAt), "server heartbeat = %s, want %s", lastReported, reportedAt)
}
