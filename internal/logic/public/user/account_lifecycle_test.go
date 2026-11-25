package user

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/node"
	usermodel "github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func newAccountFixture(t *testing.T) (*svc.ServiceContext, *ent.Client, *miniredis.Miniredis) {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	cache := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	return &svc.ServiceContext{Store: repository.NewStore(client, redisClient), Redis: redisClient}, client, cache
}

func TestDeviceUnbindRemovesSessionAndKeepsAccountWithEmail(t *testing.T) {
	service, client, cache := newAccountFixture(t)
	ctx := t.Context()
	owner := client.User.Create().SetPassword("fixture").SaveX(ctx)
	other := client.User.Create().SetPassword("fixture").SaveX(ctx)
	device := client.UserDevice.Create().SetUserID(owner.ID).SetIP("192.0.2.1").SetIdentifier("tablet").SetOnline(true).SaveX(ctx)
	client.UserDevice.Create().SetUserID(other.ID).SetIP("192.0.2.2").SetIdentifier("other-device").SaveX(ctx)
	client.UserAuthMethod.Create().SetUserID(owner.ID).SetAuthType("device").SetAuthIdentifier(device.Identifier).SaveX(ctx)
	client.UserAuthMethod.Create().SetUserID(owner.ID).SetAuthType("email").SetAuthIdentifier("owner@example.test").SaveX(ctx)
	ownerCtx := context.WithValue(ctx, constant.CtxKeyUser, &usermodel.User{Id: owner.ID})
	deviceKey := fmt.Sprintf("%s:%s", config.DeviceCacheKeyKey, device.Identifier)
	sessionKey := fmt.Sprintf("%s:tablet-session", config.SessionIdKey)
	if err := cache.Set(deviceKey, "tablet-session"); err != nil {
		t.Fatal(err)
	}
	if err := cache.Set(sessionKey, "session-data"); err != nil {
		t.Fatal(err)
	}

	list, err := NewGetDeviceListLogic(ownerCtx, service).GetDeviceList()
	if err != nil || list.Total != 1 || len(list.List) != 1 || list.List[0].Id != device.ID || !list.List[0].Online {
		t.Fatalf("device list = %+v, %v", list, err)
	}
	unbind := NewUnbindDeviceLogic(ownerCtx, service)
	request := &types.UnbindDeviceRequest{Id: device.ID}
	if err := unbind.UnbindDevice(request); err != nil {
		t.Fatal(err)
	}
	if _, err := client.UserDevice.Get(ctx, device.ID); !ent.IsNotFound(err) {
		t.Fatalf("removed device lookup error = %v", err)
	}
	if cache.Exists(deviceKey) || cache.Exists(sessionKey) {
		t.Fatal("removed device session was retained")
	}
	if _, err := client.User.Get(ctx, owner.ID); err != nil {
		t.Fatalf("email account was removed: %v", err)
	}
	methods, err := service.Store.User().FindUserAuthMethods(ctx, owner.ID)
	if err != nil || len(methods) != 1 || methods[0].AuthType != "email" {
		t.Fatalf("remaining credentials = %+v, %v", methods, err)
	}
	if err := unbind.UnbindDevice(request); err == nil {
		t.Fatal("already removed device returned success")
	}
}

func TestDeletingOwnAccountRemovesItsAccess(t *testing.T) {
	service, client, cache := newAccountFixture(t)
	ctx := t.Context()
	owner := client.User.Create().SetPassword("fixture").SaveX(ctx)
	other := client.User.Create().SetPassword("fixture").SaveX(ctx)
	plan := client.Subscribe.Create().SetName("Basic").SaveX(ctx)
	makeAccess := func(userID int64, label string) (int64, int64, int64) {
		t.Helper()
		identity := client.UserAuthMethod.Create().SetUserID(userID).SetAuthType("email").SetAuthIdentifier(label + "@example.test").SaveX(ctx)
		device := client.UserDevice.Create().SetUserID(userID).SetIdentifier(label).SetIP("192.0.2.1").SaveX(ctx)
		subscription := client.UserSubscribe.Create().SetUserID(userID).SetOrderID(userID).SetSubscribeID(plan.ID).
			SetToken(label + "-subscription").SetStatus(1).SetExpireTime(time.Now().Add(time.Hour)).SaveX(ctx)
		return identity.ID, device.ID, subscription.ID
	}
	identity, device, subscription := makeAccess(owner.ID, "owner")
	otherIdentity, otherDevice, otherSubscription := makeAccess(other.ID, "other")
	ownerCtx := context.WithValue(ctx, constant.CtxKeyUser, &usermodel.User{Id: owner.ID})
	ownerCtx = context.WithValue(ownerCtx, constant.CtxKeySessionID, "deleting-session")
	ownerKey, otherKey := fmt.Sprintf("%s:deleting-session", config.SessionIdKey), fmt.Sprintf("%s:remaining-session", config.SessionIdKey)
	if err := cache.Set(ownerKey, fmt.Sprint(owner.ID)); err != nil {
		t.Fatal(err)
	}
	if err := cache.Set(otherKey, fmt.Sprint(other.ID)); err != nil {
		t.Fatal(err)
	}
	if err := NewDeleteCurrentUserAccountLogic(ownerCtx, service).DeleteCurrentUserAccount(); err != nil {
		t.Fatal(err)
	}
	if client.User.GetX(ctx, owner.ID).DeletedAt == nil || cache.Exists(ownerKey) {
		t.Fatal("deleted account is still active or retained its session")
	}
	if _, err := client.UserAuthMethod.Get(ctx, identity); !ent.IsNotFound(err) {
		t.Fatalf("deleted account's identity lookup: %v", err)
	}
	if _, err := client.UserDevice.Get(ctx, device); !ent.IsNotFound(err) {
		t.Fatalf("deleted account's device lookup: %v", err)
	}
	if _, err := client.UserSubscribe.Get(ctx, subscription); !ent.IsNotFound(err) {
		t.Fatalf("deleted account's subscription lookup: %v", err)
	}
	if client.User.GetX(ctx, other.ID).DeletedAt != nil || !cache.Exists(otherKey) ||
		client.UserAuthMethod.GetX(ctx, otherIdentity).UserID != other.ID || client.UserDevice.GetX(ctx, otherDevice).UserID != other.ID ||
		client.UserSubscribe.GetX(ctx, otherSubscription).UserID != other.ID {
		t.Fatal("account deletion changed another customer's access")
	}
}

func TestDeviceOnlineStatisticsCombinesSessionsAndUsesHistoricalMaxima(t *testing.T) {
	service, client, _ := newAccountFixture(t)
	ctx := t.Context()
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	yesterday = time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 12, 0, 0, 0, yesterday.Location())
	addSession := func(id int64, at time.Time, seconds, days int64) {
		client.UserDeviceOnlineRecord.Create().SetUserID(id).SetIdentifier("desktop").SetOnlineTime(at).
			SetOfflineTime(at.Add(time.Duration(seconds) * time.Second)).SetCreatedAt(at).
			SetOnlineSeconds(seconds).SetDurationDays(days).SaveX(ctx)
	}
	addSession(42, yesterday, 1800, 4)
	addSession(42, yesterday.Add(time.Hour), 3600, 4)
	addSession(42, now.AddDate(0, 0, -12), 7200, 10)
	addSession(43, yesterday, 21600, 25)

	for _, tc := range []struct {
		userID, currentDays, longestDays, minutes int64
		weeklyHours                               float64
	}{{42, 4, 10, 120, 1.5}, {44, 1, 0, 0, 0}} {
		userCtx := context.WithValue(ctx, constant.CtxKeyUser, &usermodel.User{Id: tc.userID})
		got, err := NewDeviceOnlineStatisticsLogic(userCtx, service).DeviceOnlineStatistics()
		if err != nil || len(got.WeeklyStats) != 7 {
			t.Fatalf("user %d statistics = %+v, %v", tc.userID, got, err)
		}
		var hours float64
		var activeDays int
		for _, day := range got.WeeklyStats {
			hours += day.Hours
			if day.Hours > 0 {
				activeDays++
				if day.DayName != yesterday.Weekday().String() {
					t.Errorf("activity assigned to %s, want %s", day.DayName, yesterday.Weekday())
				}
			}
		}
		if hours != tc.weeklyHours || activeDays > 1 || got.ConnectionRecords.CurrentContinuousDays != tc.currentDays || got.ConnectionRecords.HistoryContinuousDays != tc.longestDays || got.ConnectionRecords.LongestSingleConnection != tc.minutes {
			t.Fatalf("user %d, weekly hours %.1f, days %d, connection records %+v", tc.userID, hours, activeDays, got.ConnectionRecords)
		}
	}
}

func TestAccountAuditHistoryFiltersByUserAndEventType(t *testing.T) {
	service, client, _ := newAccountFixture(t)
	ctx := t.Context()
	userCtx := context.WithValue(ctx, constant.CtxKeyUser, &usermodel.User{Id: 42})
	at := time.Date(2026, 1, 7, 11, 30, 0, 0, time.UTC)
	addLog := func(kind uint8, accountID int64, content string, offset time.Duration) *ent.SystemLog {
		return client.SystemLog.Create().SetType(kind).SetObjectID(accountID).SetContent(content).SetCreatedAt(at.Add(offset)).SaveX(ctx)
	}
	loginType, subscriptionType := log.TypeLogin.Uint8(), log.TypeSubscribe.Uint8()
	addLog(loginType, 42, `malformed`, 0)
	older := addLog(loginType, 42, `{"login_ip":"192.0.2.21","success":false}`, time.Hour)
	login := addLog(loginType, 42, `{"login_ip":"192.0.2.20","user_agent":"Desktop","success":true}`, 2*time.Hour)
	addLog(loginType, 43, `{"login_ip":"192.0.2.22","success":true}`, 3*time.Hour)
	sub := addLog(subscriptionType, 42, `{"user_subscribe_id":18,"client_ip":"192.0.2.30","user_agent":"Router","token":"fixture"}`, 0)
	addLog(subscriptionType, 43, `{"user_subscribe_id":19}`, time.Hour)

	logic := NewGetLoginLogLogic(userCtx, service)
	for page, want := range []*ent.SystemLog{login, older} {
		got, err := logic.GetLoginLog(&types.GetLoginLogRequest{Page: page + 1, Size: 1})
		if err != nil || got.Total != 3 || len(got.List) != 1 || got.List[0].Id != want.ID || got.List[0].Success != (page == 0) || got.List[0].Timestamp != want.CreatedAt.UnixMilli() {
			t.Fatalf("login history page %d = %+v, %v", page+1, got, err)
		}
	}
	badPage, err := logic.GetLoginLog(&types.GetLoginLogRequest{Page: 3, Size: 1})
	if err != nil || badPage.Total != 3 || len(badPage.List) != 0 {
		t.Fatalf("login history with malformed entry = %+v, %v", badPage, err)
	}
	got, err := NewGetSubscribeLogLogic(userCtx, service).GetSubscribeLog(&types.GetSubscribeLogRequest{Page: 1, Size: 10})
	if err != nil || got.Total != 1 || len(got.List) != 1 || got.List[0].Id != sub.ID || got.List[0].UserSubscribeId != 18 || got.List[0].IP != "192.0.2.30" || got.List[0].UserAgent != "Router" {
		t.Fatalf("subscription audit history = %+v, %v", got, err)
	}
}

func TestCommissionWithdrawalRecordsPendingRequestAndDebit(t *testing.T) {
	service, client, _ := newAccountFixture(t)
	ctx := t.Context()
	owner := client.User.Create().SetPassword("fixture").SaveX(ctx)
	wallet := client.UserWallet.Create().SetUserID(owner.ID).SetBalance(500).SetCommission(1000).SaveX(ctx)
	ownerCtx := context.WithValue(ctx, constant.CtxKeyUser, &usermodel.User{Id: owner.ID})
	logic := NewCommissionWithdrawLogic(ownerCtx, service)
	request := &types.CommissionWithdrawRequest{Amount: 700, Content: "Test payout account"}
	created, err := logic.CommissionWithdraw(request)
	if err != nil || created.UserId != owner.ID || created.Amount != 700 || created.Status != 0 {
		t.Fatalf("withdrawal = %+v, %v", created, err)
	}
	if _, err := logic.CommissionWithdraw(request); err == nil {
		t.Fatal("insufficient commission was accepted")
	}
	if got := client.UserWallet.GetX(ctx, wallet.ID); got.Commission != 300 || got.Balance != 500 {
		t.Fatalf("withdrawal changed wallet incorrectly: %+v", got)
	}
	other := client.User.Create().SetPassword("fixture").SaveX(ctx)
	client.UserWithdrawal.Create().SetUserID(other.ID).SetAmount(900).SetContent("Another payout account").SaveX(ctx)
	withdrawals, err := NewQueryWithdrawalLogLogic(ownerCtx, service).QueryWithdrawalLog(&types.QueryWithdrawalLogListRequest{})
	if err != nil || withdrawals.Total != 1 || len(withdrawals.List) != 1 || withdrawals.List[0].Id == 0 || withdrawals.List[0].UserId != owner.ID || withdrawals.List[0].Content != request.Content || withdrawals.List[0].Amount != 700 || withdrawals.List[0].Status != 0 {
		t.Fatalf("withdrawal history = %+v, %v", withdrawals, err)
	}
	debits, err := NewQueryUserCommissionLogLogic(ownerCtx, service).QueryUserCommissionLog(&types.QueryUserCommissionLogListRequest{Page: 1, Size: 10})
	if err != nil || debits.Total != 1 || len(debits.List) != 1 || debits.List[0].Amount != -700 || debits.List[0].UserId != owner.ID || debits.List[0].Timestamp <= 0 {
		t.Fatalf("commission history = %+v, %v", debits, err)
	}
}

func TestTrafficReportAggregatesOnlyTheRequestedSubscriptionAndFillsMissingDays(t *testing.T) {
	service, client, _ := newAccountFixture(t)
	ctx := t.Context()
	account := client.User.Create().SetPassword("fixture").SaveX(ctx)
	owner := context.WithValue(ctx, constant.CtxKeyUser, &usermodel.User{Id: account.ID})
	plan := client.Subscribe.Create().SetName("Monthly").SaveX(ctx)
	sub := client.UserSubscribe.Create().SetUserID(account.ID).SetOrderID(1).SetSubscribeID(plan.ID).SetStatus(1).SaveX(ctx)
	another := client.UserSubscribe.Create().SetUserID(account.ID).SetOrderID(2).SetSubscribeID(plan.ID).SetStatus(1).SaveX(ctx)
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	add := func(subscriptionID, upload, download int64, daysAgo int) {
		t.Helper()
		client.TrafficLog.Create().SetUserID(account.ID).SetSubscribeID(subscriptionID).SetServerID(1).
			SetUpload(upload).SetDownload(download).SetTimestamp(today.AddDate(0, 0, -daysAgo).Add(time.Hour)).SaveX(ctx)
	}
	add(sub.ID, 3, 17, 1)
	add(sub.ID, 2, 8, 1)
	add(sub.ID, 10, 20, 3)
	add(sub.ID, 80, 90, 9)
	add(another.ID, 50, 100, 1)

	logic := NewGetUserTrafficStatsLogic(owner, service)
	got, err := logic.GetUserTrafficStats(&types.GetUserTrafficStatsRequest{UserSubscribeId: fmt.Sprint(sub.ID), Days: 7})
	if err != nil || len(got.List) != 7 || got.TotalUpload != 15 || got.TotalDownload != 45 || got.TotalTraffic != 60 {
		t.Fatalf("traffic history = %+v, %v", got, err)
	}
	for i, day := range got.List {
		date := today.AddDate(0, 0, i-6)
		if day.Date != date.Format(time.DateOnly) {
			t.Fatalf("traffic day %d = %s, want %s", i, day.Date, date.Format(time.DateOnly))
		}
		var upload, download int64
		switch i {
		case 3:
			upload, download = 10, 20
		case 5:
			upload, download = 5, 25
		}
		if day.Upload != upload || day.Download != download || day.Total != upload+download {
			t.Fatalf("traffic day %s = %+v, want upload=%d download=%d", day.Date, day, upload, download)
		}
	}
	if got, err := logic.GetUserTrafficStats(&types.GetUserTrafficStatsRequest{UserSubscribeId: "invalid", Days: 7}); err == nil || got != nil {
		t.Fatalf("non-numeric subscription returned a report: %+v, %v", got, err)
	}
}

func TestReferralSummaryNetsDebitsAndOmitsArchivedReferrals(t *testing.T) {
	service, client, _ := newAccountFixture(t)
	ctx := t.Context()
	account := client.User.Create().SetPassword("fixture").SaveX(ctx)
	owner := context.WithValue(ctx, constant.CtxKeyUser, &usermodel.User{Id: account.ID})
	member := client.User.Create().SetPassword("fixture").SetRefererID(account.ID).SetEnable(true).SaveX(ctx)
	client.UserAuthMethod.Create().SetUserID(member.ID).SetAuthType("email").SetAuthIdentifier("abc@example.test").SaveX(ctx)
	client.User.Create().SetPassword("fixture").SetRefererID(account.ID).SetDeletedAt(time.Now()).SaveX(ctx)
	client.User.Create().SetPassword("fixture").SaveX(ctx)
	for _, amount := range []int64{500, 200, -250} {
		client.SystemLog.Create().SetType(log.TypeCommission.Uint8()).SetObjectID(account.ID).
			SetContent(fmt.Sprintf(`{"amount":%d}`, amount)).SaveX(ctx)
	}
	client.SystemLog.Create().SetType(log.TypeCommission.Uint8()).SetObjectID(member.ID).SetContent(`{"amount":5000}`).SaveX(ctx)
	client.SystemLog.Create().SetType(log.TypeBalance.Uint8()).SetObjectID(account.ID).
		SetContent(`{"amount":250,"balance":750,"order_no":"credit-1","timestamp":1760000000000}`).SaveX(ctx)
	summary, err := NewQueryUserAffiliateLogic(owner, service).QueryUserAffiliate()
	if err != nil || summary.Registers != 1 || summary.TotalCommission != 450 {
		t.Fatalf("referral summary = %+v, %v", summary, err)
	}
	list, err := NewQueryUserAffiliateListLogic(owner, service).QueryUserAffiliateList(&types.QueryUserAffiliateListRequest{Page: 1, Size: 10})
	if err != nil || list.Total != 1 || len(list.List) != 1 || !list.List[0].Enable || list.List[0].Identifier != "abc@e***e.test" {
		t.Fatalf("referral list = %+v, %v", list, err)
	}
	balance, err := NewQueryUserBalanceLogLogic(owner, service).QueryUserBalanceLog()
	if err != nil || balance.Total != 1 || len(balance.List) != 1 || balance.List[0].OrderNo != "credit-1" || balance.List[0].Amount != 250 || balance.List[0].Balance != 750 {
		t.Fatalf("balance history = %+v, %v", balance, err)
	}
}

func TestSubscriptionNoteAndCredentialRotationPersistAndEvictServerCache(t *testing.T) {
	service, client, cache := newAccountFixture(t)
	ctx := t.Context()
	owner := client.User.Create().SetPassword("fixture").SaveX(ctx)
	plan := client.Subscribe.Create().SetName("Monthly").SaveX(ctx)
	oldID := uuid.NewString()
	sub := client.UserSubscribe.Create().SetUserID(owner.ID).SetOrderID(1).SetSubscribeID(plan.ID).
		SetToken("old-token").SetUUID(oldID).SaveX(ctx)
	ownerCtx := context.WithValue(ctx, constant.CtxKeyUser, &usermodel.User{Id: owner.ID})
	if err := NewUpdateUserSubscribeNoteLogic(ownerCtx, service).UpdateUserSubscribeNote(&types.UpdateUserSubscribeNoteRequest{
		UserSubscribeId: sub.ID, Note: "Home router",
	}); err != nil {
		t.Fatal(err)
	}
	got := client.UserSubscribe.GetX(ctx, sub.ID)
	if got.Note != "Home router" || got.Token != sub.Token || got.UUID != oldID {
		t.Fatalf("note update affected subscription incorrectly: %+v", got)
	}

	const cacheKey = node.ServerUserListCacheKey + "3:vless:4"
	if err := cache.Set(cacheKey, "cached users"); err != nil {
		t.Fatal(err)
	}
	if err := NewResetUserSubscribeTokenLogic(ownerCtx, service).ResetUserSubscribeToken(&types.ResetUserSubscribeTokenRequest{UserSubscribeId: sub.ID}); err != nil {
		t.Fatal(err)
	}
	got = client.UserSubscribe.GetX(ctx, sub.ID)
	if _, err := uuid.Parse(got.UUID); err != nil {
		t.Fatalf("rotated node UUID: %v", err)
	}
	if got.Token == "" || got.Token == sub.Token || got.UUID == oldID || got.Note != "Home router" || cache.Exists(cacheKey) {
		t.Fatalf("rotation did not persist new credentials and evict the cache: cache still present = %t", cache.Exists(cacheKey))
	}
}
