package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"entgo.io/ent"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/middleware"
	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestOperatorCanTraceASettingsChangeToItsRequestLocation(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	admin, session := f.account(true)
	_, member := f.account(false)
	f.router = hertzx.New()
	f.router.Hertz().Use(middleware.LoggerMiddleware(f.service))
	RegisterHandlers(f.router, f.service)
	const lookup = "/v1/admin/tool/ip/location?ip=192.0.2.11"
	f.request(http.MethodGet, lookup, session, "", xerr.ERROR)
	location, err := svc.OpenIPLocation(config.GeoIPConfig{Path: filepath.Join("..", "svc", "testdata", "fixture-city.mmdb"), Required: true})
	require.NoError(t, err)
	f.service.GeoIP = location
	t.Cleanup(func() { require.NoError(t, location.Close()) })
	f.request(http.MethodGet, lookup, member, "", xerr.InvalidAccess)
	var expected types.QueryIPLocationResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, lookup, session, "", http.StatusOK), &expected))
	require.Equal(t, types.QueryIPLocationResponse{Country: "Test Country", Region: "Test Region", City: "Test City"}, expected)
	f.client.System.Create().SetCategory("log").SetKey("AutoClear").SetValue("false").SetType("bool").SetDesc("Enable cleanup").SaveX(ctx)
	f.client.System.Create().SetCategory("log").SetKey("ClearDays").SetValue("7").SetType("int64").SetDesc("Days of traffic history").SaveX(ctx)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/admin/log/setting", strings.NewReader(`{"auto_clear":true,"clear_days":21}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", session)
	req.Header.Set("User-Agent", "operations-console")
	req.Header.Set("X-Forwarded-For", "192.0.2.11")
	f.serve(req, http.StatusOK, http.StatusOK)
	var history types.FilterAdminActionLogResponse
	path := fmt.Sprintf("/v1/admin/log/admin-action/list?user_id=%d&page=1&size=10", admin.ID)
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path, session, "", http.StatusOK), &history))
	require.Len(t, history.List, 1)
	change := history.List[0]
	require.Equal(t, admin.ID, change.ActorID)
	require.Equal(t, "settings.log", change.Action)
	require.Contains(t, change.Detail, "clear_days=21")
	require.Equal(t, "operations-console", change.UserAgent)
	require.Equal(t, "192.0.2.11", change.ClientIP)
	require.Equal(t, expected.Country, change.IPCountry)
	require.Equal(t, expected.Region, change.IPRegion)
	require.Equal(t, expected.City, change.IPCity)
}

func TestOperatorCanTraceRegistrationAndLoginAttemptsForOneMember(t *testing.T) {
	f := newRouteFixture(t)
	_, operator := f.account(true)
	f.service.Config.JwtAuth.AccessExpire = 300
	const address = "reader@example.test"
	const password = "registration-audit-fixture"
	start := time.Now()
	auth := func(path, email, secret, agent string, code uint32) json.RawMessage {
		t.Helper()
		payload := fmt.Sprintf(`{"email":%q,"password":%q}`, email, secret)
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/auth/"+path, strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", agent)
		return f.serve(req, http.StatusOK, code)
	}
	var signedUp types.LoginResponse
	require.NoError(t, json.Unmarshal(auth("register", address, password, "initial-browser", http.StatusOK), &signedUp))
	auth("register", "another-reader@example.test", password, "another-browser", http.StatusOK)
	var member types.User
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/user/info", signedUp.Token, "", http.StatusOK), &member))
	auth("login", address, "mistyped-password", "returning-browser", xerr.UserPasswordError)
	auth("login", address, password, "returning-browser", http.StatusOK)
	end := time.Now()
	filter := fmt.Sprintf("&user_id=%d&start_date=%s&end_date=%s", member.Id, start.Format(time.DateOnly), end.Format(time.DateOnly))
	var registered types.FilterRegisterLogResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/admin/log/register/list?page=1&size=10"+filter, operator, "", http.StatusOK), &registered))
	require.EqualValues(t, 1, registered.Total)
	require.Len(t, registered.List, 1)
	entry := registered.List[0]
	require.Equal(t, member.Id, entry.UserId)
	require.Equal(t, address, entry.Identifier)
	require.Equal(t, "email", entry.AuthMethod)
	require.Equal(t, "initial-browser", entry.UserAgent)
	require.GreaterOrEqual(t, entry.Timestamp, start.UnixMilli())
	require.LessOrEqual(t, entry.Timestamp, end.UnixMilli())
	for i, want := range []struct {
		agent   string
		success bool
	}{{"returning-browser", true}, {"returning-browser", false}, {"initial-browser", true}} {
		var attempts types.FilterLoginLogResponse
		page := fmt.Sprintf("/v1/admin/log/login/list?page=%d&size=1", i+1)
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, page+filter, operator, "", http.StatusOK), &attempts))
		require.EqualValues(t, 3, attempts.Total)
		require.Len(t, attempts.List, 1)
		attempt := attempts.List[0]
		require.Equal(t, member.Id, attempt.UserId)
		require.Equal(t, "email", attempt.Method)
		require.Equal(t, want.agent, attempt.UserAgent)
		require.Equal(t, want.success, attempt.Success)
	}
}

func TestMemberCanReviewTheirOwnLoginAttempts(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	member, session := f.account(false)
	other, _ := f.account(false)
	created := time.Date(2026, 3, 7, 9, 0, 0, 0, time.UTC)
	write := func(userID int64, content string) int64 {
		created = created.Add(time.Minute)
		return f.client.SystemLog.Create().SetType(modellog.TypeLogin.Uint8()).SetObjectID(userID).
			SetCreatedAt(created).SetContent(content).SaveX(ctx).ID
	}
	first := write(member.ID, `{"login_ip":"192.0.2.5","user_agent":"laptop","success":false}`)
	write(other.ID, `{"login_ip":"192.0.2.8","user_agent":"another member","success":true}`)
	write(member.ID, `incomplete historical entry`)
	last := write(member.ID, `{"login_ip":"192.0.2.6","user_agent":"mobile","success":true}`)
	const history = "/v1/public/user/login_log"
	f.request(http.MethodGet, history+"?page=bad&size=10", session, "", xerr.InvalidParams)
	unavailable := true
	f.client.SystemLog.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, query ent.Query) (ent.Value, error) {
			if unavailable && ent.QueryFromContext(ctx).Op == ent.OpQueryAll {
				return nil, errors.New("history temporarily unavailable")
			}
			return next.Query(ctx, query)
		})
	}))
	require.Empty(t, f.request(http.MethodGet, history+"?page=1&size=2", session, "", xerr.DatabaseQueryError))
	unavailable = false
	for page, want := range []types.UserLoginLog{
		{Id: last, UserId: member.ID, LoginIP: "192.0.2.6", UserAgent: "mobile", Success: true, Timestamp: created.UnixMilli()},
		{Id: first, UserId: member.ID, LoginIP: "192.0.2.5", UserAgent: "laptop", Timestamp: created.Add(-3 * time.Minute).UnixMilli()},
	} {
		var response types.GetLoginLogResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, fmt.Sprintf("%s?page=%d&size=2", history, page+1), session, "", http.StatusOK), &response))
		require.EqualValues(t, 3, response.Total)
		require.Equal(t, []types.UserLoginLog{want}, response.List)
	}
}

func TestMemberInvitationHistoryMasksIdentitiesAndExcludesArchivedMembers(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	inviter, session := f.account(false)
	other, _ := f.account(false)
	registered := time.Date(2025, 11, 4, 12, 0, 0, 0, time.UTC)
	invite := func(referrer int64, avatar string, enabled bool) int64 {
		return f.client.User.Create().SetPassword("fixture").SetAvatar(avatar).SetRefererID(referrer).
			SetCreatedAt(registered).SetEnable(enabled).SaveX(ctx).ID
	}
	first := invite(inviter.ID, "original", true)
	f.client.UserAuthMethod.Create().SetUserID(first).SetAuthType("email").SetAuthIdentifier("abcdefghi").SaveX(ctx)
	invite(other.ID, "other", true)
	archived := invite(inviter.ID, "archived", true)
	f.client.User.UpdateOneID(archived).SetDeletedAt(registered.Add(time.Hour)).ExecX(ctx)
	invite(inviter.ID, "waiting", false)
	const list = "/v1/public/user/affiliate/list"
	f.request(http.MethodGet, list+"?size=invalid", session, "", xerr.InvalidParams)
	for i, want := range []types.UserAffiliate{
		{Avatar: "waiting", RegisteredAt: registered.UnixMilli()},
		{Avatar: "original", Identifier: "abc***ghi", Enable: true, RegisteredAt: registered.UnixMilli()},
	} {
		var page types.QueryUserAffiliateListResponse
		raw := f.request(http.MethodGet, fmt.Sprintf("%s?page=%d&size=1", list, i+1), session, "", http.StatusOK)
		require.NotContains(t, string(raw), "abcdefghi")
		require.NoError(t, json.Unmarshal(raw, &page))
		require.EqualValues(t, 2, page.Total)
		require.Equal(t, []types.UserAffiliate{want}, page.List)
	}
}

func TestOperatorCanReconcileOneCustomersOrderAndWalletEntries(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, admin := f.account(true)
	owner, customer := f.account(false)
	other, _ := f.account(false)
	const order = "statement-57"
	const date = "2026-08-12"
	const timestamp int64 = 1786536000000
	for _, entry := range []struct {
		kind modellog.Type
		body string
	}{
		{modellog.TypeOrderCreated, `{"order_no":"statement-57","order_type":1,"quantity":2,"price":1000,"amount":750,"gift_amount":250,"method":"balance","source":"user","timestamp":1786536000000}`},
		{modellog.TypeBalance, `{"order_no":"statement-57","type":2,"amount":-750,"balance":1250,"timestamp":1786536000000}`},
		{modellog.TypeGift, `{"order_no":"statement-57","type":1,"amount":-250,"balance":0,"subscribe_id":21,"remark":"Plan purchase","timestamp":1786536000000}`},
		{modellog.TypeCommission, `{"order_no":"statement-57","type":2,"amount":75,"timestamp":1786536000000}`},
	} {
		for _, scope := range []struct {
			id   int64
			date string
		}{{owner.ID, date}, {other.ID, date}, {owner.ID, "2026-08-11"}} {
			f.client.SystemLog.Create().SetType(entry.kind.Uint8()).SetDate(scope.date).SetObjectID(scope.id).SetContent(entry.body).SaveX(ctx)
		}
	}
	const base = "/v1/admin/log/"
	query := fmt.Sprintf("/list?page=1&size=10&user_id=%d&start_date=%s&end_date=%s", owner.ID, date, date)
	f.request(http.MethodGet, base+"order"+query, customer, "", xerr.InvalidAccess)
	f.request(http.MethodGet, base+"balance/list?start_date=invalid", admin, "", xerr.InvalidParams)
	get := func(category string, response any) {
		t.Helper()
		data := f.request(http.MethodGet, base+category+query, admin, "", http.StatusOK)
		require.NoError(t, json.Unmarshal(data, response))
	}
	var purchases types.FilterOrderLogResponse
	get("order", &purchases)
	require.EqualValues(t, 1, purchases.Total)
	require.Len(t, purchases.List, 1)
	purchase := purchases.List[0]
	require.Equal(t, order, purchase.OrderNo)
	require.Equal(t, owner.ID, purchase.UserId)
	require.EqualValues(t, 1000, purchase.Price)
	require.EqualValues(t, 750, purchase.Amount)
	require.EqualValues(t, 250, purchase.GiftAmount)
	require.Equal(t, "balance", purchase.Method)

	var balance types.FilterBalanceLogResponse
	get("balance", &balance)
	require.EqualValues(t, 1, balance.Total)
	require.Equal(t, []types.BalanceLog{{UserId: owner.ID, Type: 2, Amount: -purchase.Amount, Balance: 1250, OrderNo: order, Timestamp: timestamp}}, balance.List)
	var gifts types.FilterGiftLogResponse
	get("gift", &gifts)
	require.EqualValues(t, 1, gifts.Total)
	require.Equal(t, []types.GiftLog{{UserId: owner.ID, Type: 1, Amount: -purchase.GiftAmount, SubscribeId: 21, OrderNo: order, Remark: "Plan purchase", Timestamp: timestamp}}, gifts.List)
	var commissions types.FilterCommissionLogResponse
	get("commission", &commissions)
	require.EqualValues(t, 1, commissions.Total)
	require.Equal(t, []types.CommissionLog{{UserId: owner.ID, Type: 2, Amount: 75, OrderNo: order, Timestamp: timestamp}}, commissions.List)
}

func TestAccountStatementsRecoverAfterPartialPageReadFailure(t *testing.T) {
	f := newRouteFixture(t)
	owner, customer := f.account(false)
	other, _ := f.account(false)
	_, admin := f.account(true)
	for _, id := range []int64{owner.ID, other.ID} {
		for _, entry := range []struct {
			kind   modellog.Type
			amount int64
		}{{modellog.TypeBalance, -325}, {modellog.TypeCommission, 19}, {modellog.TypeGift, -75}, {modellog.TypeOrderCreated, 325}, {modellog.TypeUnmatchedPayment, 490}} {
			body := fmt.Sprintf(`{"order_no":"receipt-%d","amount":%d,"timestamp":101}`, id, entry.amount)
			for _, content := range []string{body, `{"order_no":`} {
				f.client.SystemLog.Create().SetType(entry.kind.Uint8()).SetDate("2026-03-01").SetObjectID(id).SetContent(content).SaveX(t.Context())
			}
		}
	}
	var failRows bool
	f.client.SystemLog.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, query ent.Query) (ent.Value, error) {
			if failRows && ent.QueryFromContext(ctx).Op == ent.OpQueryAll {
				return nil, errors.New("ledger storage temporarily unavailable")
			}
			return next.Query(ctx, query)
		})
	}))
	operatorFilter := fmt.Sprintf("/list?page=1&size=10&user_id=%d", owner.ID)
	for _, view := range []struct {
		path, session string
		amount        int64
		readError     uint32
	}{
		{"/v1/public/user/balance_log", customer, -325, xerr.DatabaseQueryError},
		{"/v1/public/user/commission_log?page=1&size=10", customer, 19, xerr.DatabaseQueryError},
		{"/v1/admin/log/balance" + operatorFilter, admin, -325, xerr.DatabaseQueryError},
		{"/v1/admin/log/commission" + operatorFilter, admin, 19, xerr.DatabaseQueryError},
		{"/v1/admin/log/gift" + operatorFilter, admin, -75, xerr.DatabaseQueryError},
		{"/v1/admin/log/order" + operatorFilter, admin, 325, xerr.DatabaseQueryError},
		{"/v1/admin/log/unmatched-payment" + operatorFilter, admin, 490, xerr.ERROR},
	} {
		failRows = true
		require.Empty(t, f.request(http.MethodGet, view.path, view.session, "", view.readError))
		failRows = false
		var result struct {
			Total int `json:"total"`
			List  []struct {
				UserID  int64  `json:"user_id,string"`
				OrderNo string `json:"order_no"`
				Amount  int64  `json:"amount"`
			} `json:"list"`
		}
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, view.path, view.session, "", http.StatusOK), &result))
		require.Equal(t, 2, result.Total)
		require.Len(t, result.List, 1)
		require.Equal(t, owner.ID, result.List[0].UserID)
		require.Equal(t, fmt.Sprintf("receipt-%d", owner.ID), result.List[0].OrderNo)
		require.Equal(t, view.amount, result.List[0].Amount)
	}
}

func TestReferralDashboardReportsTheWholeLedgerAfterStorageRecovers(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	referrer, session := f.account(false)
	member := f.client.User.Create().SetRefererID(referrer.ID).SetPassword("fixture").SaveX(ctx)
	log := func(owner, amount int64) {
		t.Helper()
		f.client.SystemLog.Create().SetType(modellog.TypeCommission.Uint8()).SetObjectID(owner).
			SetContent(fmt.Sprintf(`{"amount":%d}`, amount)).SaveX(ctx)
	}
	log(referrer.ID, 5000)
	log(referrer.ID, -200)
	for range 105 {
		log(referrer.ID, 7)
	}
	log(member.ID, 900)
	unavailable := true
	f.client.SystemLog.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, query ent.Query) (ent.Value, error) {
			if unavailable {
				return nil, errors.New("commission ledger offline")
			}
			return next.Query(ctx, query)
		})
	}))
	const path = "/v1/public/user/affiliate/count"
	require.Empty(t, f.request(http.MethodGet, path, session, "", xerr.DatabaseQueryError))
	unavailable = false
	var result types.QueryUserAffiliateCountResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path, session, "", http.StatusOK), &result))
	require.EqualValues(t, 1, result.Registers)
	require.EqualValues(t, 5535, result.TotalCommission)
}

func TestWeeklyUsageReconcilesWithOperatorDetailsAcrossMidnight(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	owner, member := f.account(false)
	_, operator := f.account(true)
	plan := f.client.Subscribe.Create().SetName("Weekly").SaveX(ctx)
	sub := f.client.UserSubscribe.Create().SetUserID(owner.ID).SetOrderID(1).SetSubscribeID(plan.ID).SaveX(ctx)
	second := f.client.UserSubscribe.Create().SetUserID(owner.ID).SetOrderID(2).SetSubscribeID(plan.ID).SaveX(ctx)
	day := time.Now().AddDate(0, 0, -2)
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	end := start.AddDate(0, 0, 1)
	logUsage := func(id int64, at time.Time, up, down int64) int64 {
		t.Helper()
		return f.client.TrafficLog.Create().SetServerID(11).SetUserID(owner.ID).SetSubscribeID(id).
			SetTimestamp(at).SetUpload(up).SetDownload(down).SaveX(ctx).ID
	}
	first := logUsage(sub.ID, start, 10, 30)
	last := logUsage(sub.ID, end.Add(-time.Nanosecond), 3, 7)
	logUsage(sub.ID, end, 4, 12)
	logUsage(sub.ID, start.AddDate(0, 0, -8), 50, 50)
	logUsage(second.ID, start, 100, 100)
	var detail types.GetUserSubscribeTrafficLogsResponse
	path := fmt.Sprintf("/v1/admin/user/subscribe/traffic_logs?user_id=%d&subscribe_id=%d&start_time=%d&end_time=%d", owner.ID, sub.ID, start.UnixMilli(), end.UnixMilli())
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path, operator, "", http.StatusOK), &detail))
	require.EqualValues(t, 2, detail.Total)
	require.Len(t, detail.List, 2)
	require.Equal(t, []int64{last, first}, []int64{detail.List[0].Id, detail.List[1].Id})
	var weekly types.GetUserTrafficStatsResponse
	path = fmt.Sprintf("/v1/public/user/traffic_stats?user_subscribe_id=%d&days=7", sub.ID)
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path, member, "", http.StatusOK), &weekly))
	require.Len(t, weekly.List, 7)
	require.Contains(t, weekly.List, types.DailyTrafficStats{Date: start.Format(time.DateOnly), Upload: 13, Download: 37, Total: 50})
	require.Contains(t, weekly.List, types.DailyTrafficStats{Date: end.Format(time.DateOnly), Upload: 4, Download: 12, Total: 16})
	require.Equal(t, [3]int64{17, 49, 66}, [3]int64{weekly.TotalUpload, weekly.TotalDownload, weekly.TotalTraffic})
}

func TestDownloadHistoryFiltersByBothAccountAndSubscription(t *testing.T) {
	f := newRouteFixture(t)
	owner, member := f.account(false)
	other, _ := f.account(false)
	_, operator := f.account(true)
	at := time.Date(2026, 8, 12, 17, 5, 3, 0, time.UTC)
	seed := func(userID, subID int64, client string) types.UserSubscribeLog {
		t.Helper()
		entry := modellog.Subscribe{UserSubscribeId: subID, UserAgent: client, ClientIP: "192.0.2.10", Token: client + "-download"}
		body, err := json.Marshal(entry)
		require.NoError(t, err)
		row := f.client.SystemLog.Create().SetType(modellog.TypeSubscribe.Uint8()).SetObjectID(userID).
			SetDate(at.Format(time.DateOnly)).SetCreatedAt(at).SetContent(string(body)).SaveX(t.Context())
		return types.UserSubscribeLog{Id: row.ID, UserId: userID, UserSubscribeId: subID, Token: entry.Token, IP: entry.ClientIP, UserAgent: client, Timestamp: at.UnixMilli()}
	}
	phone := seed(owner.ID, 12, "Phone")
	laptop := seed(owner.ID, 123, "Laptop")
	seed(other.ID, 31, "Desktop")
	read := func(path, token string, dst any) {
		t.Helper()
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path, token, "", http.StatusOK), dst))
	}
	var personal types.GetSubscribeLogResponse
	read("/v1/public/user/subscribe_log?page=1&size=10", member, &personal)
	require.EqualValues(t, 2, personal.Total)
	require.Equal(t, []types.UserSubscribeLog{laptop, phone}, personal.List)
	var history types.GetUserSubscribeLogsResponse
	read(fmt.Sprintf("/v1/admin/user/subscribe/logs?user_id=%d&subscribe_id=12", owner.ID), operator, &history)
	require.EqualValues(t, 1, history.Total)
	require.Equal(t, []types.UserSubscribeLog{phone}, history.List)
	var report types.FilterSubscribeLogResponse
	path := fmt.Sprintf("/v1/admin/log/subscribe/list?date=2026-08-12&user_subscribe_id=12&user_id=%d", owner.ID)
	read(path, operator, &report)
	require.EqualValues(t, 1, report.Total)
	require.Equal(t, []types.SubscribeLog{{UserId: owner.ID, UserSubscribeId: 12, Token: phone.Token, UserAgent: phone.UserAgent, ClientIP: phone.IP, Timestamp: phone.Timestamp}}, report.List)
}

func TestAuditPagesRecoverFromAnUnreadablePageAndSkipDamagedEntries(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	owner, member := f.account(false)
	_, operator := f.account(true)
	var failedOp string
	f.client.SystemLog.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, query ent.Query) (ent.Value, error) {
			if failedOp != "" && ent.QueryFromContext(ctx).Op == failedOp {
				return nil, errors.New("audit page unavailable")
			}
			return next.Query(ctx, query)
		})
	}))
	for _, report := range []struct {
		kind                     modellog.Type
		body, field, value       string
		operatorPath, memberPath string
	}{
		{modellog.TypeEmailMessage, `{"to":"ops@example.test","subject":"Renewal"}`, "to", "ops@example.test", "email/list", ""},
		{modellog.TypeMobileMessage, `{"to":"+12025550129","platform":"sms"}`, "to", "+12025550129", "mobile/list", ""},
		{modellog.TypeLogin, `{"method":"mobile","success":true,"user_agent":"mobile-reader"}`, "user_agent", "mobile-reader", "login/list", ""},
		{modellog.TypeRegister, `{"auth_method":"email","identifier":"signup@example.test"}`, "identifier", "signup@example.test", "register/list", ""},
		{modellog.TypeSubscribe, `{"user_agent":"subscription-reader","user_subscribe_id":43}`, "user_agent", "subscription-reader", "subscribe/list", "subscribe_log"},
		{modellog.TypeResetSubscribe, `{"order_no":"quota-reset-43","user_id":43}`, "order_no", "quota-reset-43", "subscribe/reset/list", ""},
	} {
		t.Run(report.operatorPath, func(t *testing.T) {
			fixture := *f
			fixture.t = t
			for _, body := range []string{report.body, `{"truncated":`} {
				f.client.SystemLog.Create().SetType(report.kind.Uint8()).SetObjectID(owner.ID).SetContent(body).SaveX(ctx)
			}
			views := []struct{ path, token string }{{"/v1/admin/log/" + report.operatorPath + "?page=1&size=2", operator}}
			if report.memberPath != "" {
				views = append(views, struct{ path, token string }{"/v1/public/user/" + report.memberPath + "?page=1&size=2", member})
			}
			if report.kind == modellog.TypeEmailMessage || report.kind == modellog.TypeMobileMessage {
				views = append(views, struct{ path, token string }{fmt.Sprintf("/v1/admin/log/message/list?type=%d&page=1&size=2", report.kind), operator})
			}
			for _, view := range views {
				for _, failedOp = range []string{ent.OpQueryCount, ent.OpQueryAll} {
					require.Empty(t, fixture.request(http.MethodGet, view.path, view.token, "", xerr.DatabaseQueryError))
				}
				failedOp = ""
				var page struct {
					Total int                          `json:"total"`
					List  []map[string]json.RawMessage `json:"list"`
				}
				require.NoError(t, json.Unmarshal(fixture.request(http.MethodGet, view.path, view.token, "", http.StatusOK), &page))
				require.Equal(t, 2, page.Total)
				require.Len(t, page.List, 1)
				require.JSONEq(t, fmt.Sprintf("%q", report.value), string(page.List[0][report.field]))
			}
		})
	}
}

func TestOperatorCanFindDeliveryAttemptsByChannelAndRecipient(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, admin := f.account(true)
	const date = "2026-08-12"
	at := time.Date(2026, 8, 12, 17, 5, 3, 0, time.UTC)
	for _, entry := range []struct {
		kind modellog.Type
		date string
		body string
	}{
		{modellog.TypeEmailMessage, date, `{"to":"alerts@example.test","subject":"Subscription expiry","platform":"smtp","status":1,"content":{"days":3}}`},
		{modellog.TypeEmailMessage, "2026-08-11", `{"to":"earlier@example.test","status":2}`},
		{modellog.TypeMobileMessage, date, `{"to":"+12025550123","platform":"sms","status":2,"content":{"reason":"relay unavailable"}}`},
	} {
		f.client.SystemLog.Create().SetType(entry.kind.Uint8()).SetDate(entry.date).SetCreatedAt(at).SetContent(entry.body).SaveX(ctx)
	}
	var email types.FilterEmailLogResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/admin/log/email/list?date="+date, admin, "", http.StatusOK), &email))
	require.EqualValues(t, 1, email.Total)
	require.Len(t, email.List, 1)
	require.Equal(t, "alerts@example.test", email.List[0].To)
	require.Equal(t, "Subscription expiry", email.List[0].Subject)
	require.EqualValues(t, 1, email.List[0].Status)
	require.Equal(t, map[string]any{"days": float64(3)}, email.List[0].Content)
	require.Equal(t, at.UnixMilli(), email.List[0].CreatedAt)
	var mobile types.FilterMobileLogResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/admin/log/mobile/list?date="+date, admin, "", http.StatusOK), &mobile))
	require.EqualValues(t, 1, mobile.Total)
	require.Len(t, mobile.List, 1)
	require.Equal(t, "+12025550123", mobile.List[0].To)
	require.Equal(t, "sms", mobile.List[0].Platform)
	require.EqualValues(t, 2, mobile.List[0].Status)
	require.Equal(t, map[string]any{"reason": "relay unavailable"}, mobile.List[0].Content)
	var search types.GetMessageLogListResponse
	url := fmt.Sprintf("/v1/admin/log/message/list?type=%d&search=alerts", modellog.TypeEmailMessage)
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, url, admin, "", http.StatusOK), &search))
	require.EqualValues(t, 1, search.Total)
	require.Equal(t, email.List, search.List)
}
