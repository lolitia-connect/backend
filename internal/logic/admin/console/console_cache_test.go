package console

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
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

func TestConsoleStatisticsSeparateRedemptionOrders(t *testing.T) {
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
	// Old cache entries must not preserve the previous new-purchase classification.
	cache.Set(ctx, "console:user_statistics", `{"today":{"new_order_users":99}}`, time.Minute)
	cache.Set(ctx, "console:revenue_statistics", `{"today":{"new_order_amount":99}}`, time.Minute)

	users, err := NewQueryUserStatisticsLogic(ctx, service).QueryUserStatistics()
	if err != nil {
		t.Fatal(err)
	}
	for _, period := range []struct {
		name        string
		data        types.UserStatistics
		redemptions int64
	}{{"today", users.Today, 2}, {"monthly", users.Monthly, 2}, {"all", users.All, 3}} {
		if period.data.NewOrderUsers != 1 || period.data.RenewalOrderUsers != 1 || period.data.RedemptionOrderUsers != period.redemptions {
			t.Fatalf("%s users = %+v", period.name, period.data)
		}
	}
	for _, list := range [][]types.UserStatistics{users.Monthly.List, users.All.List} {
		found := false
		for _, bucket := range list {
			if bucket.Date == now.Format("2006-01-02") || bucket.Date == now.Format("2006-01") {
				found = true
				if bucket.Register != 0 || bucket.NewOrderUsers != 1 || bucket.RenewalOrderUsers != 1 || bucket.RedemptionOrderUsers != 2 {
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
	for _, period := range []types.OrdersStatistics{revenue.Today, revenue.Monthly, revenue.All} {
		if period.AmountTotal != 330 || period.NewOrderAmount != 100 || period.RenewalOrderAmount != 200 || period.RedemptionOrderAmount != 30 {
			t.Fatalf("revenue = %+v", period)
		}
	}
	for _, list := range [][]types.OrdersStatistics{revenue.Monthly.List, revenue.All.List} {
		found := false
		for _, bucket := range list {
			if bucket.Date == now.Format("2006-01-02") || bucket.Date == now.Format("2006-01") {
				found = true
				if bucket.AmountTotal != 330 || bucket.NewOrderAmount != 100 || bucket.RenewalOrderAmount != 200 || bucket.RedemptionOrderAmount != 30 {
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
