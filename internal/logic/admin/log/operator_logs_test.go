package log

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logging"
	"github.com/perfect-panel/server/pkg/requestmeta"
	_ "modernc.org/sqlite"
)

var operatorFixtureID atomic.Uint64

func operatorLogFixture(t *testing.T) (*svc.ServiceContext, *ent.Client) {
	t.Helper()
	database, err := sql.Open("sqlite", fmt.Sprintf("file:operator-log-%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", operatorFixtureID.Add(1)))
	if err != nil {
		t.Fatal(err)
	}
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	return &svc.ServiceContext{Store: repository.NewStore(client, nil)}, client
}

func TestTrafficLogPaginationAcrossCurrentAndHistory(t *testing.T) {
	service, client := operatorLogFixture(t)
	ctx := context.Background()
	client.TrafficLog.Create().SetServerID(1).SetUserID(7).SetSubscribeID(12).SetUpload(9).SaveX(ctx)
	client.TrafficLog.Create().SetServerID(1).SetUserID(8).SetSubscribeID(120).SetUpload(99).SaveX(ctx)
	for _, content := range []string{`{"user_id":7,"subscribe_id":12,"upload":1}`, `{"user_id":8,"subscribe_id":120,"upload":99}`, `{"user_id":7,"subscribe_id":12,"upload":2}`} {
		client.SystemLog.Create().SetType(modellog.TypeSubscribeTraffic.Uint8()).SetDate(time.Now().AddDate(0, 0, -1).Format(time.DateOnly)).SetContent(content).SaveX(ctx)
	}
	for _, check := range []struct {
		page    int
		uploads []int64
	}{{1, []int64{9, 2}}, {2, []int64{1}}, {99, nil}} {
		response, err := NewFilterUserSubscribeTrafficLogLogic(ctx, service).FilterUserSubscribeTrafficLog(&types.FilterSubscribeTrafficRequest{
			FilterLogParams: types.FilterLogParams{Page: check.page, Size: 2}, UserId: 7, UserSubscribeId: 12,
		})
		if err != nil {
			t.Fatal(err)
		}
		if response.Total != 3 || len(response.List) != len(check.uploads) {
			t.Fatalf("page %d = %+v", check.page, response)
		}
		for index, upload := range check.uploads {
			if response.List[index].Upload != upload || response.List[index].UserId != 7 || response.List[index].SubscribeId != 12 {
				t.Fatalf("page %d mixed users or lost ordering: %+v", check.page, response.List)
			}
		}
	}
}

func TestServerTrafficPaginationAcrossCurrentAndHistory(t *testing.T) {
	service, client := operatorLogFixture(t)
	ctx := context.Background()
	client.TrafficLog.Create().SetServerID(1).SetUserID(7).SetSubscribeID(12).SetUpload(9).SaveX(ctx)
	client.TrafficLog.Create().SetServerID(2).SetUserID(8).SetSubscribeID(120).SetUpload(99).SaveX(ctx)
	for _, item := range []struct{ server, upload int64 }{{1, 1}, {2, 99}, {1, 2}} {
		client.SystemLog.Create().SetType(modellog.TypeServerTraffic.Uint8()).SetObjectID(item.server).
			SetDate(time.Now().AddDate(0, 0, -1).Format(time.DateOnly)).SetContent(fmt.Sprintf(`{"upload":%d}`, item.upload)).SaveX(ctx)
	}
	for _, check := range []struct {
		page, size int
		uploads    []int64
	}{{1, 2, []int64{9, 2}}, {2, 2, []int64{1}}, {99, 2, nil}, {0, 0, []int64{9, 2, 1}}} {
		t.Run(fmt.Sprintf("page-%d-size-%d", check.page, check.size), func(t *testing.T) {
			response, err := NewFilterServerTrafficLogLogic(ctx, service).FilterServerTrafficLog(&types.FilterServerTrafficLogRequest{
				FilterLogParams: types.FilterLogParams{Page: check.page, Size: check.size}, ServerId: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			if response.Total != 3 || len(response.List) != len(check.uploads) {
				t.Fatalf("unexpected combined page: %+v", response)
			}
			for index, upload := range check.uploads {
				if response.List[index].Upload != upload || response.List[index].ServerId != 1 {
					t.Fatalf("page mixed servers or lost history: %+v", response.List)
				}
			}
		})
	}
}

func TestTrafficLogDateFiltersApplyToLiveAndArchivedRows(t *testing.T) {
	service, client := operatorLogFixture(t)
	ctx := context.Background()
	now := time.Now()
	today, yesterday, earlier := now.Format(time.DateOnly), now.AddDate(0, 0, -1).Format(time.DateOnly), now.AddDate(0, 0, -2).Format(time.DateOnly)
	client.TrafficLog.Create().SetServerID(1).SetUserID(7).SetSubscribeID(12).SetUpload(9).SaveX(ctx)
	for _, date := range []string{earlier, yesterday} {
		client.SystemLog.Create().SetType(modellog.TypeServerTraffic.Uint8()).SetObjectID(1).SetDate(date).SetContent(`{"upload":2}`).SaveX(ctx)
		client.SystemLog.Create().SetType(modellog.TypeSubscribeTraffic.Uint8()).SetDate(date).SetContent(`{"user_id":7,"subscribe_id":12,"upload":2}`).SaveX(ctx)
	}
	for _, test := range []struct {
		name, date, start, end string
		want                   int64
	}{
		{"exact today", today, "", "", 1},
		{"exact historical", yesterday, "", "", 1},
		{"historical range", "", yesterday, yesterday, 1},
		{"range includes today", "", yesterday, today, 2},
		{"range overrides date", today, yesterday, yesterday, 1},
		{"open start", "", today, "", 1},
		{"open end", "", "", earlier, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			filter := types.FilterLogParams{Page: 1, Size: 10, Date: test.date, StartDate: test.start, EndDate: test.end}
			servers, err := NewFilterServerTrafficLogLogic(ctx, service).FilterServerTrafficLog(&types.FilterServerTrafficLogRequest{FilterLogParams: filter, ServerId: 1})
			if err != nil || servers.Total != test.want || int64(len(servers.List)) != test.want {
				t.Errorf("server date filter: response=%+v, err=%v, want=%d", servers, err, test.want)
			}
			users, err := NewFilterUserSubscribeTrafficLogLogic(ctx, service).FilterUserSubscribeTrafficLog(&types.FilterSubscribeTrafficRequest{FilterLogParams: filter, UserId: 7, UserSubscribeId: 12})
			if err != nil || users.Total != test.want || int64(len(users.List)) != test.want {
				t.Errorf("user date filter: response=%+v, err=%v, want=%d", users, err, test.want)
			}
		})
	}
}

func TestTrafficDetailsDateRangesIncludeWholeEndDay(t *testing.T) {
	service, client := operatorLogFixture(t)
	ctx := context.Background()
	today := time.Now()
	day := time.Date(today.Year(), today.Month(), today.Day()-1, 0, 0, 0, 0, time.Local)
	for _, timestamp := range []time.Time{day.Add(-time.Second), day, day.AddDate(0, 0, 1).Add(-time.Millisecond), day.AddDate(0, 0, 1)} {
		client.TrafficLog.Create().SetServerID(1).SetUserID(7).SetSubscribeID(12).SetTimestamp(timestamp).SaveX(ctx)
	}
	for _, test := range []struct {
		name, start, end string
		want             int64
	}{
		{"one day", day.Format(time.DateOnly), day.Format(time.DateOnly), 2},
		{"open start", day.Format(time.DateOnly), "", 3},
		{"open end", "", day.Format(time.DateOnly), 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, err := NewFilterTrafficLogDetailsLogic(ctx, service).FilterTrafficLogDetails(&types.FilterTrafficLogDetailsRequest{
				FilterLogParams: types.FilterLogParams{Page: 1, Size: 10, Date: today.Format(time.DateOnly), StartDate: test.start, EndDate: test.end}, ServerId: 1,
			})
			if err != nil || response.Total != test.want || int64(len(response.List)) != test.want {
				t.Fatalf("detail date filter: response=%+v, err=%v, want=%d", response, err, test.want)
			}
		})
	}
}

func TestOperatorLogFiltersKeepActorsAndPayersSeparate(t *testing.T) {
	service, _ := operatorLogFixture(t)
	ctx := context.Background()
	for _, actorID := range []int64{7, 8} {
		entry, err := modellog.NewAdminActionLog(requestmeta.WithActor(ctx, actorID), modellog.AdminAction{Action: "ticket.reply", ObjectID: 12})
		if err != nil {
			t.Fatal(err)
		}
		if err := service.Store.Log().Insert(ctx, entry); err != nil {
			t.Fatal(err)
		}
		if err := service.Store.Log().RecordUnmatchedPayment(ctx, actorID, modellog.UnmatchedPayment{TradeNo: fmt.Sprint(actorID), Reason: "amount mismatch"}); err != nil {
			t.Fatal(err)
		}
	}
	actions, err := FilterAdminActionLog(ctx, service, &types.FilterAdminActionLogRequest{UserId: 7})
	if err != nil || actions.Total != 1 || len(actions.List) != 1 || actions.List[0].UserId != 7 || actions.List[0].ObjectId != 12 {
		t.Fatalf("actor filter = %+v, %v", actions, err)
	}
	payments, err := FilterUnmatchedPaymentLog(ctx, service, &types.FilterUnmatchedPaymentLogRequest{UserId: 8})
	if err != nil || payments.Total != 1 || len(payments.List) != 1 || payments.List[0].UserId != 8 || payments.List[0].TradeNo != "8" {
		t.Fatalf("payer filter = %+v, %v", payments, err)
	}
}

func TestSentrySettingsSurviveCleanupOnlyWrites(t *testing.T) {
	service, client := operatorLogFixture(t)
	ctx := requestmeta.WithActor(context.Background(), 7)
	read := NewGetLogSettingLogic(ctx, service)
	write := NewUpdateLogSettingLogic(ctx, service)
	cfg, err := read.GetLogSetting()
	if err != nil || cfg.SentryEnabled == nil || *cfg.SentryEnabled || cfg.SentryDSN != "" {
		t.Fatalf("unexpected defaults: %+v, %v", cfg, err)
	}
	enabled := true
	const dsn = "http://test-key@127.0.0.1:1/42"
	defer logging.CloseSentry()
	if err := write.UpdateLogSetting(&types.LogSetting{SentryEnabled: &enabled, SentryDSN: dsn}); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"AutoClear": "false", "ClearDays": "7"} {
		client.System.Create().SetCategory("log").SetKey(key).SetValue(value).SetDesc(key).SaveX(ctx)
	}
	if err := write.UpdateLogSetting(&types.LogSetting{AutoClear: &enabled, ClearDays: 14}); err != nil {
		t.Fatal(err)
	}
	if err := write.UpdateLogSetting(&types.LogSetting{SentryEnabled: &enabled, SentryDSN: "https://key:secret@invalid.test/42"}); err == nil {
		t.Fatal("accepted a secret-bearing DSN")
	}
	entries := client.SystemLog.Query().AllX(ctx)
	if len(entries) != 2 {
		t.Fatalf("expected two successful saves, got %d", len(entries))
	}
	for _, entry := range entries {
		if strings.Contains(entry.Content, "test-key") {
			t.Fatal("DSN in audit record")
		}
	}
	cfg, err = read.GetLogSetting()
	if err != nil || cfg.SentryEnabled == nil || !*cfg.SentryEnabled || cfg.SentryDSN != dsn {
		t.Fatalf("legacy or invalid update changed reporting: %+v, %v", cfg, err)
	}
}

func TestRetentionChangeRequiresAuditAndSevenDays(t *testing.T) {
	service, client := operatorLogFixture(t)
	ctx := requestmeta.WithActor(context.Background(), 7)
	enabled := true
	logic := NewUpdateLogSettingLogic(ctx, service)
	if err := logic.UpdateLogSetting(&types.LogSetting{AutoClear: &enabled, ClearDays: 6}); err == nil {
		t.Fatal("short retention accepted")
	}
	for key, value := range map[string]string{"AutoClear": "false", "ClearDays": "7"} {
		client.System.Create().SetCategory("log").SetKey(key).SetValue(value).SetDesc(key).SaveX(ctx)
	}
	if err := logic.UpdateLogSetting(&types.LogSetting{AutoClear: &enabled, ClearDays: 14}); err != nil {
		t.Fatal(err)
	}
	if client.SystemLog.Query().CountX(ctx) != 1 || service.Config.Log.ClearDays != 14 {
		t.Fatal("retention update was not audited")
	}
	if err := client.Driver().Exec(ctx, "DROP TABLE system_logs", []any{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := logic.UpdateLogSetting(&types.LogSetting{AutoClear: &enabled, ClearDays: 30}); err == nil {
		t.Fatal("retention committed without audit")
	}
	stored, err := service.Store.System().FindByCategoryKey(ctx, "log", "ClearDays")
	if err != nil || stored.Value != "14" || service.Config.Log.ClearDays != 14 {
		t.Fatalf("failed retention update changed state: %+v, %v", stored, err)
	}
}
