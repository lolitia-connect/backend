package traffic

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/config"
	paneltypes "github.com/perfect-panel/server/internal/types"
)

func TestServerDashboardCachesRankedDailyTrafficAndMonthlyTotals(t *testing.T) {
	service, _ := trafficStatTestService(t)
	ctx := t.Context()
	db := service.Store.Ent()
	primary := db.Server.Create().SetName("Primary").SaveX(ctx)
	backup := db.Server.Create().SetName("Backup").SaveX(ctx)
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterday := today.AddDate(0, 0, -1)
	add := func(server, user, upload, download int64, at time.Time) {
		t.Helper()
		db.TrafficLog.Create().SetServerID(server).SetUserID(user).SetSubscribeID(user).
			SetUpload(upload).SetDownload(download).SetTimestamp(at).SaveX(ctx)
	}
	add(primary.ID, 101, 10, 200, today.Add(time.Hour))
	add(primary.ID, 101, 20, 90, today.Add(2*time.Hour))
	add(backup.ID, 102, 40, 30, today.Add(time.Hour))
	add(backup.ID, 102, 50, 150, yesterday.Add(time.Hour))
	add(primary.ID, 101, 90, 100, today.AddDate(0, -2, 0))

	if err := NewServerDataLogic(service).ProcessTask(ctx, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := service.Redis.Get(ctx, config.ServerCountCacheKey).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var got paneltypes.ServerTotalDataResponse
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	monthlyUpload, monthlyDownload := int64(70), int64(320)
	if yesterday.Month() == today.Month() {
		monthlyUpload += 50
		monthlyDownload += 150
	}
	if got.TodayUpload != 70 || got.TodayDownload != 320 || got.MonthlyUpload != monthlyUpload || got.MonthlyDownload != monthlyDownload {
		t.Fatalf("daily and monthly totals = %+v", got)
	}
	if servers := got.ServerTrafficRankingToday; len(servers) != 2 || servers[0].ServerId != primary.ID || servers[0].Name != "Primary" || servers[0].Upload != 30 || servers[0].Download != 290 || servers[1].ServerId != backup.ID {
		t.Fatalf("today's server rankings = %+v", servers)
	}
	if users := got.UserTrafficRankingToday; len(users) != 2 || users[0].SID != 101 || users[0].Upload != 30 || users[0].Download != 290 || users[1].SID != 102 {
		t.Fatalf("today's user rankings = %+v", users)
	}
	if servers := got.ServerTrafficRankingYesterday; len(servers) != 1 || servers[0].ServerId != backup.ID || servers[0].Download != 150 {
		t.Fatalf("yesterday's server rankings = %+v", servers)
	}
	if users := got.UserTrafficRankingYesterday; len(users) != 1 || users[0].SID != 102 || users[0].Upload != 50 {
		t.Fatalf("yesterday's user rankings = %+v", users)
	}
}
