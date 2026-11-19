package user

import (
	"context"
	"testing"
	"time"

	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/types"
)

func TestUserSubscribeLogsFilterAndDecodeContent(t *testing.T) {
	fixture, client, _ := basicInfoFixture(t, "")
	ctx := context.Background()
	for _, item := range []struct {
		user, subscription int64
		typ                uint8
	}{
		{7, 12, modellog.TypeSubscribe.Uint8()},
		{8, 12, modellog.TypeSubscribe.Uint8()},
		{7, 120, modellog.TypeSubscribe.Uint8()},
		{7, 12, modellog.TypeLogin.Uint8()},
		{7, 12, modellog.TypeSubscribe.Uint8()},
	} {
		content, err := (&modellog.Subscribe{UserSubscribeId: item.subscription, Token: "fixture-token", ClientIP: "192.0.2.1", UserAgent: "test-agent"}).Marshal()
		if err != nil {
			t.Fatal(err)
		}
		client.SystemLog.Create().SetType(item.typ).SetObjectID(item.user).SetDate(time.Now().Format(time.DateOnly)).SetContent(string(content)).SaveX(ctx)
	}
	for _, page := range []int{1, 2, 3} {
		response, err := NewGetUserSubscribeLogsLogic(ctx, fixture.svcCtx).GetUserSubscribeLogs(&types.GetUserSubscribeLogsRequest{UserId: 7, SubscribeId: 12, Page: page, Size: 1})
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if page == 3 {
			want = 0
		}
		if response.Total != 2 || len(response.List) != want {
			t.Fatalf("page %d mixed log types/users or ignored paging: %+v", page, response)
		}
		if want == 0 {
			continue
		}
		entry := response.List[0]
		if entry.UserId != 7 || entry.UserSubscribeId != 12 || entry.Token != "fixture-token" || entry.IP != "192.0.2.1" || entry.UserAgent != "test-agent" || entry.Timestamp == 0 {
			t.Fatalf("subscription log content was not decoded: %+v", entry)
		}
	}
}

func TestAdminCreateUserPersistsLoginMethodsAndWallet(t *testing.T) {
	for _, request := range []types.CreateUserRequest{
		{Email: "new-user@example.test", Password: "test-password", Balance: 1200, GiftAmount: 300, Commission: 500},
		{TelephoneAreaCode: "1", Telephone: "2025550123", Password: "test-password"},
	} {
		fixture, client, _ := basicInfoFixture(t, "")
		ctx := context.Background()
		if err := NewCreateUserLogic(ctx, fixture.svcCtx).CreateUser(&request); err != nil {
			t.Fatal(err)
		}
		method, err := client.UserAuthMethod.Query().Only(ctx)
		if err != nil {
			t.Fatalf("created account cannot log in: %v", err)
		}
		want := request.Email
		if want == "" {
			want = "+12025550123"
		}
		if method.AuthIdentifier != want {
			t.Fatalf("login identifier = %q, want %q", method.AuthIdentifier, want)
		}
		wallet, err := fixture.svcCtx.Store.Wallet().FindOne(ctx, method.UserID)
		if err != nil || wallet.Balance != request.Balance || wallet.GiftAmount != request.GiftAmount || wallet.Commission != request.Commission {
			t.Fatalf("opening wallet = %+v, %v", wallet, err)
		}
	}
}

func TestAdminCreateUserRollsBackLoginMethodFailure(t *testing.T) {
	fixture, client, _ := basicInfoFixture(t, "")
	if err := client.Driver().Exec(context.Background(), `CREATE TRIGGER reject_identity BEFORE INSERT ON user_auth_methods BEGIN SELECT RAISE(ABORT, 'identity insert failed'); END`, []any{}, nil); err != nil {
		t.Fatal(err)
	}
	request := &types.CreateUserRequest{Email: "new-user@example.test", Password: "test-password", Balance: 1200}
	if err := NewCreateUserLogic(context.Background(), fixture.svcCtx).CreateUser(request); err == nil {
		t.Fatal("login method failure was ignored")
	}
	if client.User.Query().CountX(context.Background()) != 1 || client.UserWallet.Query().CountX(context.Background()) != 1 {
		t.Fatal("failed creation left an orphan account or wallet")
	}
}

func TestUserSubscribeTrafficLogsApplyTimeWindow(t *testing.T) {
	fixture, client, _ := basicInfoFixture(t, "")
	ctx := context.Background()
	start := time.Date(2026, 1, 2, 0, 0, 0, 0, time.Local)
	end := start.Add(2 * time.Hour)
	for _, timestamp := range []time.Time{start.Add(-time.Hour), start, start.Add(time.Hour), end} {
		client.TrafficLog.Create().SetUserID(7).SetSubscribeID(12).SetServerID(1).SetTimestamp(timestamp).SetUpload(5).SaveX(ctx)
	}
	client.TrafficLog.Create().SetUserID(8).SetSubscribeID(12).SetServerID(1).SetTimestamp(start).SaveX(ctx)
	client.TrafficLog.Create().SetUserID(7).SetSubscribeID(13).SetServerID(1).SetTimestamp(start).SaveX(ctx)
	logic := NewGetUserSubscribeTrafficLogsLogic(ctx, fixture.svcCtx)
	for _, test := range []struct{ start, end, total int64 }{
		{start.UnixMilli(), end.UnixMilli(), 2},
		{start.UnixMilli(), 0, 3},
		{0, end.UnixMilli(), 3},
	} {
		response, err := logic.GetUserSubscribeTrafficLogs(&types.GetUserSubscribeTrafficLogsRequest{UserId: 7, SubscribeId: 12, StartTime: test.start, EndTime: test.end, Page: 1, Size: 1})
		if err != nil || response.Total != test.total || len(response.List) != 1 {
			t.Fatalf("traffic time window: response=%+v, err=%v, want=%d", response, err, test.total)
		}
	}
	if _, err := logic.GetUserSubscribeTrafficLogs(&types.GetUserSubscribeTrafficLogsRequest{UserId: 7, SubscribeId: 12, StartTime: end.UnixMilli(), EndTime: start.UnixMilli()}); err == nil {
		t.Fatal("reversed time range accepted")
	}
}

func TestUserSubscriptionsRespectPagination(t *testing.T) {
	fixture, client, _ := basicInfoFixture(t, "")
	ctx := context.Background()
	for _, id := range []int64{11, 12, 13} {
		client.UserSubscribe.Create().SetID(id).SetUserID(7).SetOrderID(id).SetSubscribeID(1).SetExpireTime(time.Now().AddDate(0, 1, 0)).SetStatus(1).SaveX(ctx)
	}
	for _, test := range []struct{ page, size, count int }{{1, 2, 2}, {2, 2, 1}, {3, 2, 0}, {0, 0, 3}} {
		response, err := NewGetUserSubscribeLogic(ctx, fixture.svcCtx).GetUserSubscribe(&types.GetUserSubscribeListRequest{UserId: 7, Page: test.page, Size: test.size})
		if err != nil || response.Total != 3 || len(response.List) != test.count {
			t.Fatalf("page %d: response=%+v, err=%v", test.page, response, err)
		}
	}
}
