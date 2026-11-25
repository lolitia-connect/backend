package console

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func TestConsoleStatisticsReturnCachedResponses(t *testing.T) {
	t.Setenv("PPANEL_MODE", "production")
	cache := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	service := &svc.ServiceContext{Redis: redisClient}
	ctx := t.Context()
	for _, entry := range []struct{ key, data string }{
		{consoleUserStatisticsCacheKey, `{"today":{"register":7}}`},
		{consoleRevenueStatisticsCacheKey, `{"today":{"amount_total":2400}}`},
		{consoleServerTotalDataCacheKey, `{"online_users":12}`},
	} {
		if err := redisClient.Set(ctx, entry.key, entry.data, 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	users, err := NewQueryUserStatisticsLogic(ctx, service).QueryUserStatistics()
	if err != nil || users.Today.Register != 7 {
		t.Fatalf("cached user statistics = %+v, %v", users, err)
	}
	revenue, err := NewQueryRevenueStatisticsLogic(ctx, service).QueryRevenueStatistics()
	if err != nil || revenue.Today.AmountTotal != 2400 {
		t.Fatalf("cached revenue statistics = %+v, %v", revenue, err)
	}
	servers, err := NewQueryServerTotalDataLogic(ctx, service).QueryServerTotalData()
	if err != nil || servers.OnlineUsers != 12 {
		t.Fatalf("cached server statistics = %+v, %v", servers, err)
	}
}

func TestServerDashboardAggregatesLiveTrafficAndArchive(t *testing.T) {
	t.Setenv("PPANEL_MODE", "production")
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, cache), Redis: cache}

	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	online := client.Server.Create().SetName("Primary").SetLastReportedAt(now).SaveX(ctx)
	offline := client.Server.Create().SetName("Backup").SetLastReportedAt(now.Add(-time.Hour)).SaveX(ctx)
	client.Server.Create().SetName("Never reported").SaveX(ctx)
	if err := cache.ZAdd(ctx, node.OnlineUserSubscribeCacheKeyWithGlobal,
		redis.Z{Score: float64(now.Add(time.Minute).Unix()), Member: "101"},
		redis.Z{Score: float64(now.Add(-time.Minute).Unix()), Member: "102"},
	).Err(); err != nil {
		t.Fatal(err)
	}
	if err := cache.Set(ctx, consoleServerTotalDataCacheKey, "{invalid", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	addTraffic := func(server, subscription, upload, download int64, at time.Time) {
		t.Helper()
		client.TrafficLog.Create().SetServerID(server).SetUserID(subscription).SetSubscribeID(subscription).
			SetUpload(upload).SetDownload(download).SetTimestamp(at).SaveX(ctx)
	}
	addTraffic(online.ID, 101, 10, 200, now)
	addTraffic(online.ID, 101, 20, 90, now)
	addTraffic(offline.ID, 102, 40, 30, now)
	addTraffic(offline.ID, 102, 50, 150, yesterday)
	monthlyUpload, monthlyDownload := int64(70), int64(320)
	if yesterday.Month() == now.Month() {
		client.SystemLog.Create().SetDate(yesterday.Format(time.DateOnly)).SetType(modellog.TypeTrafficStat.Uint8()).
			SetContent(`{"upload":60,"download":180}`).SaveX(ctx)
		monthlyUpload += 60
		monthlyDownload += 180
	}
	logic := NewQueryServerTotalDataLogic(ctx, service)
	got, err := logic.QueryServerTotalData()
	if err != nil {
		t.Fatal(err)
	}
	if got.OnlineUsers != 1 || got.OnlineServers != 1 || got.OfflineServers != 2 || got.TodayUpload != 70 ||
		got.TodayDownload != 320 || got.MonthlyUpload != monthlyUpload || got.MonthlyDownload != monthlyDownload {
		t.Fatalf("dashboard totals = %+v", got)
	}
	if servers := got.ServerTrafficRankingToday; len(servers) != 2 || servers[0].ServerId != online.ID ||
		servers[0].Name != "Primary" || servers[0].Upload != 30 || servers[0].Download != 290 || servers[1].ServerId != offline.ID {
		t.Fatalf("today's server rankings = %+v", servers)
	}
	if servers := got.ServerTrafficRankingYesterday; len(servers) != 1 || servers[0].ServerId != offline.ID ||
		servers[0].Name != "Backup" || servers[0].Download != 150 {
		t.Fatalf("yesterday's server rankings = %+v", servers)
	}
	if users := got.UserTrafficRankingToday; len(users) != 2 || users[0].SID != 101 || users[0].Download != 290 || users[1].SID != 102 {
		t.Fatalf("today's user rankings = %+v", users)
	}
	if users := got.UserTrafficRankingYesterday; len(users) != 1 || users[0].SID != 102 || users[0].Upload != 50 {
		t.Fatalf("yesterday's user rankings = %+v", users)
	}
	raw, err := cache.Get(ctx, consoleServerTotalDataCacheKey).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var cached types.ServerTotalDataResponse
	if err := json.Unmarshal(raw, &cached); err != nil || !reflect.DeepEqual(&cached, got) {
		t.Fatalf("dashboard cache does not match the computed summary: %v", err)
	}
}

func TestConsoleStatisticsSeparateRechargeAndRedemptionOrders(t *testing.T) {
	t.Setenv("PPANEL_MODE", "production")
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil), Redis: cache}
	ctx := t.Context()
	now := time.Now()
	// No registrations in these buckets: existing users can still redeem.
	for i, data := range []struct {
		userID int64
		kind   uint8
		isNew  bool
		amount int64
		status uint8
	}{
		{1, order.TypeRedemption, true, 0, order.StatusPaid},
		{1, order.TypeRedemption, false, 0, order.StatusFinished},
		{2, order.TypeRedemption, true, 30, order.StatusFinished},
		{3, order.TypeSubscribe, true, 100, order.StatusFinished},
		{4, order.TypeRenewal, false, 200, order.StatusPaid},
		{6, order.TypeRecharge, false, 40, order.StatusPaid},
		{6, order.TypeRecharge, true, 60, order.StatusFinished},
		{3, order.TypeRecharge, true, 70, order.StatusFinished},
		{5, order.TypeRecharge, false, 400, order.StatusPending},
		{5, order.TypeRecharge, true, 500, order.StatusClosed},
		{5, order.TypeRedemption, true, 400, order.StatusPending},
		{6, order.TypeRedemption, false, 500, order.StatusClosed},
	} {
		client.Order.Create().SetOrderNo(fmt.Sprintf("kpi-%d", i)).SetUserID(data.userID).
			SetType(data.kind).SetIsNew(data.isNew).SetAmount(data.amount).
			SetStatus(data.status).SetMethod("redemption-test").SetCreatedAt(now).SaveX(ctx)
	}
	// An order outside this month is included only in lifetime totals.
	client.Order.Create().SetOrderNo("kpi-previous-month").SetUserID(7).
		SetType(order.TypeRedemption).SetIsNew(true).SetStatus(order.StatusFinished).
		SetCreatedAt(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, 0, -1)).SaveX(ctx)
	client.Order.Create().SetOrderNo("kpi-previous-month-recharge").SetUserID(8).
		SetType(order.TypeRecharge).SetIsNew(true).SetAmount(80).SetStatus(order.StatusFinished).
		SetCreatedAt(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, 0, -1)).SaveX(ctx)
	// Ignore caches produced before recharge and redemption had separate categories.
	for _, suffix := range []string{"", ":v2"} {
		cache.Set(ctx, "console:user_statistics"+suffix, `{"today":{"renewal_order_users":99}}`, time.Minute)
		cache.Set(ctx, "console:revenue_statistics"+suffix, `{"today":{"renewal_order_amount":99}}`, time.Minute)
	}

	users, err := NewQueryUserStatisticsLogic(ctx, service).QueryUserStatistics()
	if err != nil {
		t.Fatal(err)
	}
	for _, period := range []struct {
		name        string
		data        types.UserStatistics
		recharges   int64
		redemptions int64
	}{{"today", users.Today, 2, 2}, {"monthly", users.Monthly, 2, 2}, {"all", users.All, 3, 3}} {
		if period.data.NewOrderUsers != 1 || period.data.RenewalOrderUsers != 1 || period.data.RechargeOrderUsers != period.recharges || period.data.RedemptionOrderUsers != period.redemptions {
			t.Fatalf("%s users = %+v", period.name, period.data)
		}
	}
	for _, list := range [][]types.UserStatistics{users.Monthly.List, users.All.List} {
		found := false
		for _, bucket := range list {
			if bucket.Date == now.Format("2006-01-02") || bucket.Date == now.Format("2006-01") {
				found = true
				if bucket.Register != 0 || bucket.NewOrderUsers != 1 || bucket.RenewalOrderUsers != 1 || bucket.RechargeOrderUsers != 2 || bucket.RedemptionOrderUsers != 2 {
					t.Fatalf("user chart bucket = %+v", bucket)
				}
			}
		}
		if !found {
			t.Fatalf("missing order-only chart bucket: %+v", list)
		}
	}
	revenue, err := NewQueryRevenueStatisticsLogic(ctx, service).QueryRevenueStatistics()
	if err != nil {
		t.Fatal(err)
	}
	for _, period := range []struct {
		data     types.OrdersStatistics
		recharge int64
	}{{revenue.Today, 170}, {revenue.Monthly, 170}, {revenue.All, 250}} {
		if period.data.AmountTotal != 330+period.recharge || period.data.NewOrderAmount != 100 || period.data.RenewalOrderAmount != 200 || period.data.RechargeOrderAmount != period.recharge || period.data.RedemptionOrderAmount != 30 {
			t.Fatalf("revenue = %+v", period.data)
		}
	}
	for _, list := range [][]types.OrdersStatistics{revenue.Monthly.List, revenue.All.List} {
		found := false
		for _, bucket := range list {
			if bucket.Date == now.Format("2006-01-02") || bucket.Date == now.Format("2006-01") {
				found = true
				if bucket.AmountTotal != 500 || bucket.NewOrderAmount != 100 || bucket.RenewalOrderAmount != 200 || bucket.RechargeOrderAmount != 170 || bucket.RedemptionOrderAmount != 30 {
					t.Fatalf("revenue chart bucket = %+v", bucket)
				}
			}
		}
		if !found {
			t.Fatalf("missing revenue chart bucket: %+v", list)
		}
	}
	for _, data := range []struct {
		userID int64
		want   bool
	}{{1, true}, {3, false}} {
		got, err := service.Store.Order().IsUserEligibleForNewOrder(ctx, data.userID)
		if err != nil || got != data.want {
			t.Fatalf("new purchase eligibility for %d = %v, %v", data.userID, got, err)
		}
	}
}
