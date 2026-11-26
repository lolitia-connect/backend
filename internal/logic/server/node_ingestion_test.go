package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func nodeLogicFixture(t *testing.T, protocols ...node.Protocol) (*svc.ServiceContext, *ent.Server, *miniredis.Miniredis) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	cache := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { _ = rds.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, rds), Redis: rds}
	data := &node.Server{}
	require.NoError(t, data.MarshalProtocols(protocols))
	server := client.Server.Create().SetName("Report node").SetProtocols(data.Protocols).SaveX(t.Context())
	return service, server, cache
}

func TestNodeRosterAppliesRecentTrafficLimitsAndRefreshesOnPlanChange(t *testing.T) {
	ctx := t.Context()
	service, server, _ := nodeLogicFixture(t, node.Protocol{Type: "vless", Id: "edge", Enable: true})
	db := service.Store.Ent()
	endpoint := db.Node.Create().SetServerID(server.ID).SetProtocol("vless").SetProtocolID("edge").SaveX(ctx)
	plan := db.Subscribe.Create().SetNodes(fmt.Sprint(endpoint.ID)).SetSpeedLimit(80).SetDeviceLimit(2).
		SetTrafficLimit(`[{"stat_type":"hour","stat_value":1,"traffic_usage":1,"speed_limit":12},{"stat_type":"day","stat_value":1,"traffic_usage":2,"speed_limit":25}]`).SaveX(ctx)
	now := time.Now()
	member := func() *ent.UserSubscribe {
		t.Helper()
		owner := db.User.Create().SetPassword("fixture").SetEnable(true).SaveX(ctx)
		return db.UserSubscribe.Create().SetUserID(owner.ID).SetSubscribeID(plan.ID).SetOrderID(owner.ID).
			SetExpireTime(now.Add(24 * time.Hour)).SetStatus(1).SetUUID(fmt.Sprintf("00000000-0000-4000-8000-%012d", owner.ID)).SaveX(ctx)
	}
	hourly, daily, ordinary := member(), member(), member()
	const gib = 1 << 30
	record := func(sub *ent.UserSubscribe, ago time.Duration, upload, download int64) {
		t.Helper()
		db.TrafficLog.Create().SetUserID(sub.UserID).SetSubscribeID(sub.ID).SetServerID(server.ID).
			SetTimestamp(now.Add(-ago)).SetUpload(upload).SetDownload(download).SaveX(ctx)
	}
	record(hourly, 10*time.Minute, gib/2, gib/2)
	record(daily, 3*time.Hour, gib, gib)
	record(ordinary, 48*time.Hour, gib, gib)
	query := &types.GetServerUserListRequest{ServerCommon: types.ServerCommon{ServerId: server.ID, Protocol: "vless", ProtocolId: "edge"}}
	roster := func(etag string) ([]types.ServerUser, string) {
		t.Helper()
		logic := NewGetServerUserListLogic(ctx, service, RequestMeta{IfNoneMatch: etag})
		result, err := logic.GetServerUserList(query)
		require.NoError(t, err)
		return result.Users, logic.ResponseMeta().Headers["ETag"]
	}
	users, etag := roster("")
	require.ElementsMatch(t, []types.ServerUser{
		{Id: hourly.ID, UUID: hourly.UUID, DeviceLimit: 2, SpeedLimit: 12},
		{Id: daily.ID, UUID: daily.UUID, DeviceLimit: 2, SpeedLimit: 25},
		{Id: ordinary.ID, UUID: ordinary.UUID, DeviceLimit: 2, SpeedLimit: 80},
	}, users)
	require.NotEmpty(t, etag)
	db.Subscribe.UpdateOneID(plan.ID).SetSpeedLimit(10).ExecX(ctx)
	require.NoError(t, service.Store.Node().ClearServerCache(ctx, server.ID))
	updated, updatedTag := roster(etag)
	require.NotEqual(t, etag, updatedTag)
	require.Len(t, updated, 3)
	for _, item := range updated {
		require.EqualValues(t, 10, item.SpeedLimit)
	}
}

func TestNodeRosterRetriesWithoutCachingIncompleteMembership(t *testing.T) {
	for _, assignment := range []string{"group", "tag"} {
		t.Run(assignment, func(t *testing.T) {
			ctx := t.Context()
			service, server, cache := nodeLogicFixture(t, node.Protocol{Type: "vless", Id: "west", Enable: true})
			db := service.Store.Ent()
			endpoint := db.Node.Create().SetServerID(server.ID).SetProtocol("vless").SetProtocolID("west")
			plan := db.Subscribe.Create().SetName("Western region").SetSpeedLimit(35).SetDeviceLimit(2)
			if assignment == "group" {
				group := db.NodeGroup.Create().SetName("West").SaveX(ctx)
				endpoint.SetNodeGroupIds([]int64{group.ID})
				plan.SetNodeGroupID(group.ID)
			} else {
				endpoint.SetTags("west")
				plan.SetNodeTags("west")
			}
			endpoint.SaveX(ctx)
			published := plan.SaveX(ctx)
			var want []types.ServerUser
			for range 2 {
				owner := db.User.Create().SetPassword("fixture").SetEnable(true).SaveX(ctx)
				sub := db.UserSubscribe.Create().SetUserID(owner.ID).SetSubscribeID(published.ID).SetOrderID(owner.ID).
					SetExpireTime(time.Now().Add(24 * time.Hour)).SetStatus(1).SetUUID(fmt.Sprintf("00000000-0000-4000-8000-%012d", owner.ID)).SaveX(ctx)
				want = append(want, types.ServerUser{Id: sub.ID, UUID: sub.UUID, SpeedLimit: 35, DeviceLimit: 2})
			}
			unavailable := errors.New("roster data unavailable")
			var blocked string
			db.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
				return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
					switch q.(type) {
					case *ent.NodeQuery:
						if blocked == "nodes" {
							return nil, unavailable
						}
					case *ent.SubscribeQuery:
						if blocked == "plans" {
							return nil, unavailable
						}
					case *ent.UserSubscribeQuery:
						if blocked == "subscriptions" {
							return nil, unavailable
						}
					case *ent.UserQuery:
						if blocked == "accounts" {
							return nil, unavailable
						}
					}
					return next.Query(ctx, q)
				})
			}))
			query := &types.GetServerUserListRequest{ServerCommon: types.ServerCommon{ServerId: server.ID, Protocol: "vless", ProtocolId: "west"}}
			key := fmt.Sprintf("%s%d:vless:west", node.ServerUserListCacheKey, server.ID)
			for _, blocked = range []string{"nodes", "plans", "subscriptions", "accounts"} {
				list, err := NewGetServerUserListLogic(ctx, service, RequestMeta{}).GetServerUserList(query)
				require.ErrorIs(t, err, unavailable, blocked)
				require.Nil(t, list)
				require.False(t, cache.Exists(key), "published a partial roster while %s were unreadable", blocked)
			}
			blocked = ""
			reader := NewGetServerUserListLogic(ctx, service, RequestMeta{})
			list, err := reader.GetServerUserList(query)
			require.NoError(t, err)
			require.ElementsMatch(t, want, list.Users)
			etag := reader.ResponseMeta().Headers["ETag"]
			require.NotEmpty(t, etag)
			blocked = "accounts"
			list, err = NewGetServerUserListLogic(ctx, service, RequestMeta{IfNoneMatch: etag}).GetServerUserList(query)
			require.ErrorIs(t, err, xerr.StatusNotModified)
			require.Nil(t, list)
		})
	}
}

func TestNodeRosterMatchesDirectAndTaggedPlansAfterPlanEdit(t *testing.T) {
	ctx := t.Context()
	service, west, _ := nodeLogicFixture(t, node.Protocol{Type: "vless", Id: "edge", Enable: true})
	db := service.Store.Ent()
	east := db.Server.Create().SetName("East").SaveX(ctx)
	westNode := db.Node.Create().SetServerID(west.ID).SetProtocol("vless").SetProtocolID("edge").SetTags("travel,west,").SaveX(ctx)
	eastNode := db.Node.Create().SetServerID(east.ID).SetProtocol("vless").SetProtocolID("edge").SetTags("east").SaveX(ctx)
	assign := func(name, nodes, tags string) (*ent.Subscribe, types.ServerUser) {
		t.Helper()
		plan := db.Subscribe.Create().SetName(name).SetNodes(nodes).SetNodeTags(tags).SetSpeedLimit(25).SaveX(ctx)
		owner := db.User.Create().SetPassword("fixture").SetEnable(true).SaveX(ctx)
		sub := db.UserSubscribe.Create().SetUserID(owner.ID).SetSubscribeID(plan.ID).SetOrderID(owner.ID).
			SetExpireTime(time.Now().Add(24 * time.Hour)).SetStatus(1).SetUUID(fmt.Sprintf("00000000-0000-4000-8000-%012d", owner.ID)).SaveX(ctx)
		return plan, types.ServerUser{Id: sub.ID, UUID: sub.UUID, SpeedLimit: plan.SpeedLimit, DeviceLimit: plan.DeviceLimit}
	}
	move, direct := assign("Direct", fmt.Sprint(westNode.ID), "")
	_, tagged := assign("Tagged", "", "travel")
	_, both := assign("Overlapping assignment", fmt.Sprint(westNode.ID), "west")
	_, regional := assign("Eastern region", "", "east")
	roster := func(serverID int64, previousETag string, want ...types.ServerUser) string {
		t.Helper()
		reader := NewGetServerUserListLogic(ctx, service, RequestMeta{IfNoneMatch: previousETag})
		result, err := reader.GetServerUserList(&types.GetServerUserListRequest{ServerCommon: types.ServerCommon{ServerId: serverID, Protocol: "vless", ProtocolId: "edge"}})
		require.NoError(t, err)
		require.ElementsMatch(t, want, result.Users)
		etag := reader.ResponseMeta().Headers["ETag"]
		require.NotEmpty(t, etag)
		require.NotEqual(t, previousETag, etag)
		return etag
	}
	westTag := roster(west.ID, "", direct, tagged, both)
	eastTag := roster(east.ID, "", regional)
	plans := service.Store.Subscribe()
	edited, err := plans.FindOne(ctx, move.ID)
	require.NoError(t, err)
	edited.Nodes = fmt.Sprint(eastNode.ID)
	require.NoError(t, plans.Update(ctx, edited))
	roster(west.ID, westTag, tagged, both)
	roster(east.ID, eastTag, regional, direct)
}

func TestGroupedNodeRosterIncludesEligibleGracePeriodSubscriptions(t *testing.T) {
	ctx := t.Context()
	service, server, _ := nodeLogicFixture(t, node.Protocol{Type: "vless", Id: "normal", Enable: true}, node.Protocol{Type: "vless", Id: "grace", Enable: true})
	db := service.Store.Ent()
	normal := db.NodeGroup.Create().SetName("Regular service").SaveX(ctx)
	grace := db.NodeGroup.Create().SetName("Expiry grace").SetIsExpiredGroup(true).SetExpiredDaysLimit(2).SetMaxTrafficGBExpired(1).SetSpeedLimit(3).SaveX(ctx)
	db.Node.Create().SetServerID(server.ID).SetProtocol("vless").SetProtocolID("normal").SetNodeGroupIds([]int64{normal.ID}).SaveX(ctx)
	db.Node.Create().SetServerID(server.ID).SetProtocol("vless").SetProtocolID("grace").SetNodeGroupIds([]int64{grace.ID}).SaveX(ctx)
	plan := db.Subscribe.Create().SetName("Regional").SetNodeGroupID(normal.ID).SetNodeGroupIds([]int64{grace.ID}).SetSpeedLimit(80).SetDeviceLimit(2).SaveX(ctx)
	other := db.Subscribe.Create().SetName("Different region").SaveX(ctx)
	now := time.Now()
	member := func(planID int64, status uint8, expiry time.Duration, expiredTraffic int64) *ent.UserSubscribe {
		t.Helper()
		owner := db.User.Create().SetPassword("fixture").SetEnable(true).SaveX(ctx)
		return db.UserSubscribe.Create().SetUserID(owner.ID).SetSubscribeID(planID).SetOrderID(owner.ID).
			SetExpireTime(now.Add(expiry)).SetStatus(status).SetExpiredDownload(expiredTraffic).
			SetUUID(fmt.Sprintf("00000000-0000-4000-8000-%012d", owner.ID)).SaveX(ctx)
	}
	const gib = 1 << 30
	active := member(plan.ID, 1, 24*time.Hour, 0)
	eligible := member(plan.ID, 3, -36*time.Hour, gib-1)
	member(plan.ID, 3, -36*time.Hour, gib)
	member(plan.ID, 3, -84*time.Hour, 0)
	member(other.ID, 1, 24*time.Hour, 0)
	roster := func(instance string) []types.ServerUser {
		t.Helper()
		req := &types.GetServerUserListRequest{ServerCommon: types.ServerCommon{ServerId: server.ID, Protocol: "vless", ProtocolId: instance}}
		response, err := NewGetServerUserListLogic(ctx, service, RequestMeta{}).GetServerUserList(req)
		require.NoError(t, err)
		return response.Users
	}
	regularUser := types.ServerUser{Id: active.ID, UUID: active.UUID, SpeedLimit: 80, DeviceLimit: 2}
	require.Equal(t, []types.ServerUser{regularUser}, roster("normal"))
	require.ElementsMatch(t, []types.ServerUser{regularUser, {Id: eligible.ID, UUID: eligible.UUID, SpeedLimit: 3}}, roster("grace"))
	db.UserSubscribe.UpdateOneID(eligible.ID).AddExpiredUpload(1).ExecX(ctx)
	require.NoError(t, service.Store.Node().ClearServerCache(ctx, server.ID))
	require.Equal(t, []types.ServerUser{regularUser}, roster("grace"))
}

func TestServerStatusPersistsHeartbeatAndInvalidatesOnPinRotation(t *testing.T) {
	ctx := t.Context()
	service, server, cache := nodeLogicFixture(t, node.Protocol{Type: "trojan", Id: "edge", Enable: true, CertMode: "self"})
	cacheKey := fmt.Sprintf("%s%d:trojan:edge", node.ServerConfigCacheKey, server.ID)
	require.NoError(t, cache.Set(cacheKey, "old config"))
	old := time.Now().Add(-time.Hour).UTC()
	client := service.Store.Ent()
	client.Server.UpdateOneID(server.ID).SetLastReportedAt(old).SaveX(ctx)
	request := &types.ServerPushStatusRequest{
		ServerCommon: types.ServerCommon{ServerId: server.ID, Protocol: "trojan", ProtocolId: "edge"},
		Cpu:          0.25, Mem: 0.5, Disk: 0.75, UpdatedAt: old.Unix(), CertPinSHA256: strings.Repeat("A1", 32),
	}
	logic := NewServerPushStatusLogic(ctx, service)
	require.NoError(t, logic.ServerPushStatus(request))
	status, err := service.Store.Node().StatusCache(ctx, server.ID)
	require.NoError(t, err)
	require.Equal(t, node.Status{Cpu: 0.25, Mem: 0.5, Disk: 0.75, UpdatedAt: old.Unix()}, status)
	saved := client.Server.GetX(ctx, server.ID)
	require.NotNil(t, saved.LastReportedAt)
	require.True(t, saved.LastReportedAt.After(old.Add(30*time.Minute)), "heartbeat should reflect receipt time")
	stored := &node.Server{Protocols: *saved.Protocols}
	protocols, err := stored.UnmarshalProtocols()
	require.NoError(t, err)
	require.Len(t, protocols, 1)
	require.Equal(t, strings.ToLower(request.CertPinSHA256), protocols[0].CertPinSHA256)
	require.False(t, cache.Exists(cacheKey))

	require.NoError(t, cache.Set(cacheKey, "current config"))
	require.NoError(t, logic.ServerPushStatus(request))
	value, err := cache.Get(cacheKey)
	require.NoError(t, err)
	require.Equal(t, "current config", value, "an unchanged pin should preserve the cache")

	beforeFailure := client.Server.GetX(ctx, server.ID)
	cache.SetError("ERR unavailable")
	request.CertPinSHA256 = strings.Repeat("B2", 32)
	request.Cpu = 0.9
	require.EqualError(t, logic.ServerPushStatus(request), "update node status failed")
	cache.SetError("")
	afterFailure := client.Server.GetX(ctx, server.ID)
	require.Equal(t, beforeFailure.LastReportedAt, afterFailure.LastReportedAt)
	require.Equal(t, beforeFailure.Protocols, afterFailure.Protocols)
	unchanged, err := service.Store.Node().StatusCache(ctx, server.ID)
	require.NoError(t, err)
	require.Equal(t, status, unchanged)
}

func TestNodeReportsAndDeviceAccessUseEligibleSubscriptions(t *testing.T) {
	ctx := t.Context()
	service, server, cache := nodeLogicFixture(t,
		node.Protocol{Type: "vless", Id: "primary", Enable: true, Ratio: 1.5},
		node.Protocol{Type: "vless", Id: "disabled"},
	)
	client := service.Store.Ent()
	endpoint := client.Node.Create().SetServerID(server.ID).SetProtocol("vless").SetProtocolID("primary").SaveX(ctx)
	plan := client.Subscribe.Create().SetNodes(fmt.Sprint(endpoint.ID)).SetDeviceLimit(1).SetSpeedLimit(20).SaveX(ctx)
	otherPlan := client.Subscribe.Create().SetNodes(fmt.Sprint(endpoint.ID + 1)).SaveX(ctx)
	createSubscription := func(planID int64, enabled bool, expires time.Time) *ent.UserSubscribe {
		owner := client.User.Create().SetPassword("test-password").SetEnable(enabled).SaveX(ctx)
		return client.UserSubscribe.Create().SetUserID(owner.ID).SetOrderID(owner.ID).SetSubscribeID(planID).
			SetUUID(fmt.Sprintf("00000000-0000-4000-8000-%012d", owner.ID)).SetStatus(1).SetExpireTime(expires).SaveX(ctx)
	}
	now := time.Now()
	active := createSubscription(plan.ID, true, now.Add(time.Hour))
	disabled := createSubscription(plan.ID, false, now.Add(time.Hour))
	expired := createSubscription(plan.ID, true, now.Add(-time.Hour))
	outside := createSubscription(otherPlan.ID, true, now.Add(time.Hour))
	common := types.ServerCommon{ServerId: server.ID, Protocol: "vless", ProtocolId: "primary"}
	query := &types.GetServerUserListRequest{ServerCommon: common}
	logic := NewGetServerUserListLogic(ctx, service, RequestMeta{})
	list, err := logic.GetServerUserList(query)
	require.NoError(t, err)
	require.Equal(t, []types.ServerUser{{Id: active.ID, UUID: active.UUID, DeviceLimit: 1, SpeedLimit: 20}}, list.Users)
	tag := logic.ResponseMeta().Headers["ETag"]
	require.NotEmpty(t, tag)
	_, err = NewGetServerUserListLogic(ctx, service, RequestMeta{IfNoneMatch: tag}).GetServerUserList(query)
	require.ErrorIs(t, err, xerr.StatusNotModified)

	online := NewPushOnlineUsersLogic(ctx, service)
	report := &types.OnlineUsersRequest{ServerCommon: common, Users: []types.OnlineUser{
		{SID: active.ID, IP: "::ffff:192.0.2.10"}, {SID: active.ID, IP: "192.0.2.10"},
		{SID: disabled.ID, IP: "192.0.2.11"}, {SID: outside.ID, IP: "192.0.2.12"},
	}}
	require.NoError(t, online.PushOnlineUsers(report))
	snapshot, err := service.Store.Node().OnlineUserSubscribe(ctx, server.ID, "vless:primary")
	require.NoError(t, err)
	require.Equal(t, node.OnlineUserSubscribe{active.ID: {"192.0.2.10"}}, snapshot)
	global, err := service.Store.Node().OnlineUserSubscribeGlobal(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, global)
	report.Users = []types.OnlineUser{{SID: active.ID, IP: "192.0.2.20"}, {SID: active.ID, IP: "::"}}
	require.ErrorContains(t, online.PushOnlineUsers(report), "invalid user data")
	current, err := service.Store.Node().OnlineUserSubscribe(ctx, server.ID, "vless:primary")
	require.NoError(t, err)
	require.Equal(t, snapshot, current)

	devices := DeviceService{Service: service}
	alive, err := devices.GetServerAliveList(ctx, query)
	require.NoError(t, err)
	require.Equal(t, map[int64]int64{active.ID: 1}, alive.Alive)
	claim := &types.ClaimServerDeviceRequest{ServerCommon: common, SID: outside.ID, IP: "192.0.2.12"}
	_, err = devices.ClaimServerDevice(ctx, claim)
	var accessErr *xerr.CodeError
	require.ErrorAs(t, err, &accessErr)
	require.Equal(t, xerr.InvalidAccess, accessErr.GetErrCode())
	require.False(t, cache.Exists(node.DeviceLeaseKey(outside.ID)))
	claim.SID = active.ID
	decision, err := devices.ClaimServerDevice(ctx, claim)
	require.NoError(t, err)
	require.False(t, decision.Allowed, "online report should reserve the only device slot")
	cache.FastForward(node.Expiry + time.Second)
	alive, err = devices.GetServerAliveList(ctx, query)
	require.NoError(t, err)
	require.Empty(t, alive.Alive)
	decision, err = devices.ClaimServerDevice(ctx, claim)
	require.NoError(t, err)
	require.True(t, decision.Allowed, "an expired lease should free the device slot")

	traffic := NewServerPushUserTrafficLogic(ctx, service)
	batch := &types.ServerPushUserTrafficRequest{ServerCommon: common, Traffic: []types.UserTraffic{
		{SID: active.ID, Upload: 20, Download: 6}, {SID: active.ID, Upload: -1, Download: 100},
		{SID: outside.ID, Upload: 100}, {SID: disabled.ID, Download: 100}, {SID: expired.ID, Upload: 100},
	}}
	require.NoError(t, traffic.ServerPushUserTraffic(batch))
	batch.ProtocolId = "disabled"
	require.ErrorContains(t, traffic.ServerPushUserTraffic(batch), "enabled protocol instance")
	batch.ServerId = server.ID + 1
	require.ErrorContains(t, traffic.ServerPushUserTraffic(batch), "traffic server not found")
	require.NoError(t, NewTrafficAggregator(service).Flush(ctx, time.Now().Add(2*time.Minute)))
	stored := client.UserSubscribe.GetX(ctx, active.ID)
	require.EqualValues(t, 30, stored.Upload)
	require.EqualValues(t, 9, stored.Download)
	log := client.TrafficLog.Query().OnlyX(ctx)
	require.Equal(t, active.ID, log.SubscribeID)
	require.Equal(t, server.ID, log.ServerID)
}
