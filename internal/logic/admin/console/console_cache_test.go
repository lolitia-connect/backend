package console

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/redis/go-redis/v9"
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
