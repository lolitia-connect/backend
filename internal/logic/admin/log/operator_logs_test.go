package log

import (
	"context"
	"database/sql"
	"fmt"
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
