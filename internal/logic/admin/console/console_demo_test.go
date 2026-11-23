package console

import (
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/svc"
)

func TestDemoConsoleStatisticsHaveStableShapesAndChronologicalSeries(t *testing.T) {
	t.Setenv("PPANEL_MODE", "DeMo")
	ctx := t.Context()
	service := &svc.ServiceContext{}

	users, err := NewQueryUserStatisticsLogic(ctx, service).QueryUserStatistics()
	if err != nil || users.Today.Register != 28 || users.Monthly.Register != 888 || len(users.Monthly.List) != 7 || len(users.All.List) != 6 {
		t.Fatalf("demo user statistics = %+v, %v", users, err)
	}
	for i := 1; i < len(users.Monthly.List); i++ {
		if users.Monthly.List[i-1].Date >= users.Monthly.List[i].Date {
			t.Fatalf("daily user series is not chronological: %+v", users.Monthly.List)
		}
	}
	if users.Monthly.List[6].Date != time.Now().Format("2006-01-02") || users.Monthly.List[6].Register <= 0 {
		t.Fatalf("latest daily user sample = %+v", users.Monthly.List[6])
	}

	revenue, err := NewQueryRevenueStatisticsLogic(ctx, service).QueryRevenueStatistics()
	if err != nil || revenue.Today.AmountTotal != 35888 || revenue.Monthly.AmountTotal != 888888 || len(revenue.Monthly.List) != 7 || len(revenue.All.List) != 6 {
		t.Fatalf("demo revenue statistics = %+v, %v", revenue, err)
	}
	for i := 1; i < len(revenue.All.List); i++ {
		if revenue.All.List[i-1].Date >= revenue.All.List[i].Date {
			t.Fatalf("monthly revenue series is not chronological: %+v", revenue.All.List)
		}
	}

	servers, err := NewQueryServerTotalDataLogic(ctx, service).QueryServerTotalData()
	if err != nil || servers.OnlineUsers != 1688 || servers.OnlineServers != 8 || servers.OfflineServers != 2 || len(servers.ServerTrafficRankingToday) != 10 || len(servers.ServerTrafficRankingYesterday) != 10 {
		t.Fatalf("demo server statistics = %+v, %v", servers, err)
	}
	for i, server := range servers.ServerTrafficRankingToday {
		if server.ServerId != int64(i+1) || server.Name == "" || server.Upload <= 0 || server.Download <= 0 {
			t.Fatalf("invalid server ranking entry %d: %+v", i, server)
		}
	}
}
