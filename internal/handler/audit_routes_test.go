package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent"
	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

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
		}{{modellog.TypeBalance, -325}, {modellog.TypeCommission, 19}} {
			body := fmt.Sprintf(`{"order_no":"receipt-%d","amount":%d,"timestamp":101}`, id, entry.amount)
			f.client.SystemLog.Create().SetType(entry.kind.Uint8()).SetDate("2026-03-01").SetObjectID(id).SetContent(body).SaveX(t.Context())
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
	}{
		{"/v1/public/user/balance_log", customer, -325},
		{"/v1/public/user/commission_log?page=1&size=10", customer, 19},
		{"/v1/admin/log/balance" + operatorFilter, admin, -325},
		{"/v1/admin/log/commission" + operatorFilter, admin, 19},
	} {
		failRows = true
		require.Empty(t, f.request(http.MethodGet, view.path, view.session, "", xerr.DatabaseQueryError))
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
		require.Equal(t, 1, result.Total)
		require.Len(t, result.List, 1)
		require.Equal(t, owner.ID, result.List[0].UserID)
		require.Equal(t, fmt.Sprintf("receipt-%d", owner.ID), result.List[0].OrderNo)
		require.Equal(t, view.amount, result.List[0].Amount)
	}
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
