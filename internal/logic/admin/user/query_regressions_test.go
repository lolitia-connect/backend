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

func TestAdminUserListCombinesIdentityWalletAndActiveSubscriptionFilters(t *testing.T) {
	fixture, client, _ := basicInfoFixture(t, "")
	ctx := t.Context()
	const memberID int64 = 7
	plan := client.Subscribe.Create().SetName("Membership").SaveX(ctx)
	membership := client.UserSubscribe.Create().SetUserID(memberID).SetSubscribeID(plan.ID).SetOrderID(1).SetStatus(1).SaveX(ctx)
	client.UserAuthMethod.Create().SetUserID(memberID).SetAuthType("email").SetAuthIdentifier("team_member@example.test").SaveX(ctx)
	client.UserAuthMethod.Create().SetUserID(memberID).SetAuthType("mobile").SetAuthIdentifier("+12025550123").SaveX(ctx)
	expired := client.User.Create().SetPassword("fixture").SaveX(ctx)
	client.UserAuthMethod.Create().SetUserID(expired.ID).SetAuthType("email").SetAuthIdentifier("teamXmember@example.test").SaveX(ctx)
	client.UserSubscribe.Create().SetUserID(expired.ID).SetSubscribeID(plan.ID).SetOrderID(2).SetStatus(3).SaveX(ctx)

	list := NewGetUserListLogic(ctx, fixture.svcCtx)
	for _, query := range []types.GetUserListRequest{
		{Page: 1, Size: 1, Search: "team_"},
		{Page: 1, Size: 1, SubscribeId: &plan.ID},
		{Page: 1, Size: 1, UserSubscribeId: &membership.ID},
	} {
		result, err := list.GetUserList(&query)
		if err != nil || result.Total != 1 || len(result.List) != 1 || result.List[0].Id != memberID {
			t.Fatalf("filtered admin account list = %+v, %v", result, err)
		}
		member := result.List[0]
		methods := make(map[string]string)
		for _, m := range member.AuthMethods {
			methods[m.AuthType] = m.AuthIdentifier
		}
		if member.Balance != 1200 || member.GiftAmount != 300 || member.Commission != 500 || methods["email"] != "team_member@example.test" || methods["mobile"] != "+1 202-555-0123" {
			t.Fatalf("account projection = %+v", member)
		}
	}
	detail, err := NewGetUserDetailLogic(ctx, fixture.svcCtx).GetUserDetail(&types.GetDetailRequest{Id: memberID})
	if err != nil || detail.Id != memberID || detail.Balance != 1200 || detail.ReferCode != "referral" {
		t.Fatalf("admin user detail = %+v, %v", detail, err)
	}

	if err := NewDeleteUserLogic(ctx, fixture.svcCtx).DeleteUser(&types.GetDetailRequest{Id: memberID}); err != nil {
		t.Fatal(err)
	}
	archived := client.User.GetX(ctx, memberID)
	if archived.DeletedAt == nil {
		t.Fatal("user was not soft-deleted")
	}
	for _, unscoped := range []bool{false, true} {
		result, err := list.GetUserList(&types.GetUserListRequest{Page: 1, Size: 1, Search: "team_", Unscoped: unscoped})
		want := 0
		if unscoped {
			want = 1
		}
		if err != nil || result.Total != int64(want) || len(result.List) != want {
			t.Fatalf("admin account list (unscoped = %t) = %+v, %v", unscoped, result, err)
		}
	}
}

func TestAdminSubscriptionGrantPauseAndUpdate(t *testing.T) {
	fixture, client, _ := basicInfoFixture(t, "")
	service := fixture.svcCtx
	service.Config.Subscribe.SingleModel = true
	ctx := t.Context()
	group := client.NodeGroup.Create().SetName("Members").SaveX(ctx)
	plan := client.Subscribe.Create().SetName("Monthly").SetNodeGroupID(group.ID).SaveX(ctx)
	upgraded := client.Subscribe.Create().SetName("Premium").SaveX(ctx)
	now := time.Now()
	grant := NewCreateUserSubscribeLogic(ctx, service)
	request := &types.CreateUserSubscribeRequest{UserId: 7, SubscribeId: plan.ID, ExpiredAt: now.Add(24 * time.Hour).UnixMilli(), Traffic: 4096}
	if err := grant.CreateUserSubscribe(request); err != nil {
		t.Fatal(err)
	}
	if err := grant.CreateUserSubscribe(request); err == nil {
		t.Fatal("single-plan account accepted a second subscription")
	}
	original := client.UserSubscribe.Query().OnlyX(ctx)
	if original.UserID != request.UserId || original.SubscribeID != plan.ID || original.NodeGroupID != group.ID || original.Traffic != 4096 || original.Token == "" || original.UUID == "" || original.Status != 1 {
		t.Fatal("grant did not persist the requested plan, quota, and credentials")
	}
	toggle := NewToggleUserSubscribeStatusLogic(ctx, service)
	for _, want := range []uint8{5, 1} {
		if err := toggle.ToggleUserSubscribeStatus(&types.ToggleUserSubscribeStatusRequest{UserSubscribeId: original.ID}); err != nil {
			t.Fatal(err)
		}
		if got := client.UserSubscribe.GetX(ctx, original.ID).Status; got != want {
			t.Fatalf("subscription state = %d, want %d", got, want)
		}
	}

	expiredAt := now.Add(-time.Hour).UnixMilli()
	if err := NewUpdateUserSubscribeLogic(ctx, service).UpdateUserSubscribe(&types.UpdateUserSubscribeRequest{
		UserSubscribeId: original.ID, SubscribeId: upgraded.ID, ExpiredAt: expiredAt, Traffic: 8192, Upload: 120, Download: 340,
	}); err != nil {
		t.Fatal(err)
	}
	current := client.UserSubscribe.GetX(ctx, original.ID)
	if current.SubscribeID != upgraded.ID || current.ExpireTime.UnixMilli() != expiredAt || current.Status != 3 ||
		current.Traffic != 8192 || current.Upload != 120 || current.Download != 340 || current.Token != original.Token {
		t.Fatal("changing plan and expiry incorrectly affected subscription state")
	}
	if err := toggle.ToggleUserSubscribeStatus(&types.ToggleUserSubscribeStatusRequest{UserSubscribeId: current.ID}); err == nil {
		t.Fatal("expired subscription was toggled to an active state")
	}
	if err := NewDeleteUserSubscribeLogic(ctx, service).DeleteUserSubscribe(&types.DeleteUserSubscribeRequest{UserSubscribeId: current.ID}); err != nil {
		t.Fatal(err)
	}
	if count := client.UserSubscribe.Query().CountX(ctx); count != 0 {
		t.Fatalf("subscription count after deletion = %d", count)
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
