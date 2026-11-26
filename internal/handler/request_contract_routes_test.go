package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	framework "entgo.io/ent"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestBulkRemovalRequiresACompleteListOfStringIDs(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	member, session := f.account(false)
	coupon := f.client.Coupon.Create().SetName("Renewal offer").SetCode("RENEW").SaveX(ctx)
	document := f.client.Document.Create().SetTitle("Setup").SetShow(true).SaveX(ctx)
	plan := f.client.Subscribe.Create().SetName("Monthly").SaveX(ctx)
	group := f.client.SubscribeGroup.Create().SetName("Standard plans").SaveX(ctx)
	code := f.client.RedemptionCode.Create().SetCode("TEST-REDEEM").SaveX(ctx)
	for _, target := range []struct {
		path     string
		id       int64
		retained func() bool
	}{
		{"coupon", coupon.ID, func() bool { return f.client.Coupon.GetX(ctx, coupon.ID).Code == coupon.Code }},
		{"document", document.ID, func() bool { return f.client.Document.GetX(ctx, document.ID).Show }},
		{"redemption/code", code.ID, func() bool { return f.client.RedemptionCode.GetX(ctx, code.ID).Code == code.Code }},
		{"subscribe", plan.ID, func() bool { return f.client.Subscribe.GetX(ctx, plan.ID).Name == plan.Name }},
		{"subscribe/group", group.ID, func() bool { return f.client.SubscribeGroup.GetX(ctx, group.ID).Name == group.Name }},
		{"user", member.ID, func() bool { return f.client.User.GetX(ctx, member.ID).DeletedAt == nil }},
	} {
		t.Run(target.path, func(t *testing.T) {
			path := "/v1/admin/" + target.path + "/batch"
			for _, body := range []string{`{}`, fmt.Sprintf(`{"ids":["%d",false]}`, target.id), fmt.Sprintf(`{"ids":["%d"`, target.id), fmt.Sprintf(`{"ids":["%d","invalid"]}`, target.id), fmt.Sprintf(`{"ids":["%d","9223372036854775808"]}`, target.id)} {
				require.Empty(t, f.request(http.MethodDelete, path, operator, body, xerr.InvalidParams))
				require.True(t, target.retained(), "invalid selection changed a selected record")
			}
		})
	}
	f.request(http.MethodGet, "/v1/public/user/info", session, "", http.StatusOK)
}

func TestAccountAndCatalogEditorsRejectLossyEntityIDs(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	owner, member := f.account(false)
	const id = 9007199254740993
	plan := f.client.Subscribe.Create().SetID(id).SetName("Original plan").SaveX(ctx)
	roundedPlan := f.client.Subscribe.Create().SetID(id - 1).SetName("Neighboring plan").SaveX(ctx)
	sub := f.client.UserSubscribe.Create().SetID(id).SetOrderID(1).SetUserID(owner.ID).SetSubscribeID(plan.ID).
		SetToken("initial-token").SetStatus(1).SetDownload(128).SaveX(ctx)
	neighbor := f.client.UserSubscribe.Create().SetID(id - 1).SetOrderID(2).SetUserID(owner.ID).SetSubscribeID(roundedPlan.ID).
		SetToken("neighbor-token").SetStatus(1).SetDownload(256).SaveX(ctx)
	for _, tc := range []struct{ method, path, field string }{
		{"PUT", "admin/subscribe/", "id"}, {"DELETE", "admin/subscribe/", "id"},
		{"PUT", "admin/subscribe/group", "id"}, {"DELETE", "admin/subscribe/group", "id"},
		{"PUT", "admin/announcement/", "id"}, {"DELETE", "admin/announcement/", "id"},
		{"PUT", "admin/payment/", "id"}, {"DELETE", "admin/payment/", "id"},
		{"PUT", "admin/application/subscribe_application", "id"}, {"DELETE", "admin/application/subscribe_application", "id"},
		{"PUT", "admin/coupon/", "id"}, {"DELETE", "admin/coupon/", "id"},
		{"PUT", "admin/document/", "id"}, {"DELETE", "admin/document/", "id"},
		{"PUT", "admin/group/node", "id"}, {"DELETE", "admin/group/node", "id"},
		{"PUT", "admin/redemption/code", "id"}, {"DELETE", "admin/redemption/code", "id"},
		{"POST", "admin/server/update", "id"}, {"POST", "admin/server/delete", "id"},
		{"POST", "admin/server/node/update", "id"}, {"POST", "admin/server/node/delete", "id"},
		{"POST", "admin/server/node/status/toggle", "id"},
		{"PUT", "admin/user/basic", "user_id"}, {"PUT", "admin/user/notify", "user_id"},
		{"POST", "admin/user/auth_method", "user_id"}, {"PUT", "admin/user/auth_method", "user_id"}, {"DELETE", "admin/user/auth_method", "user_id"},
		{"DELETE", "admin/user/device", "id"}, {"PUT", "admin/user/device/kick_offline", "id"},
		{"POST", "admin/user/subscribe", "user_id"}, {"PUT", "admin/user/subscribe", "user_subscribe_id"}, {"DELETE", "admin/user/subscribe", "user_subscribe_id"},
		{"POST", "admin/user/subscribe/reset/token", "user_subscribe_id"}, {"POST", "admin/user/subscribe/reset/traffic", "user_subscribe_id"},
		{"POST", "admin/user/subscribe/toggle", "user_subscribe_id"}, {"PUT", "public/user/subscribe_note", "user_subscribe_id"},
		{"PUT", "public/user/subscribe_token", "user_subscribe_id"}, {"POST", "public/user/unsubscribe", "id"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			token := operator
			if strings.HasPrefix(tc.path, "public/") {
				token = member
			}
			for _, value := range []string{`9007199254740992`, `"9223372036854775808"`} {
				body := fmt.Sprintf(`{"%s":%s,"name":"Changed","note":"Changed"}`, tc.field, value)
				require.Empty(t, f.request(tc.method, "/v1/"+tc.path, token, body, xerr.InvalidParams))
			}
		})
	}
	require.Equal(t, plan.Name, f.client.Subscribe.GetX(ctx, plan.ID).Name)
	current := f.client.UserSubscribe.GetX(ctx, sub.ID)
	require.Equal(t, sub.Token, current.Token)
	require.Equal(t, sub.Download, current.Download)
	require.Equal(t, sub.Status, current.Status)
	f.request(http.MethodPut, "/v1/public/user/subscribe_note", member, `{"user_subscribe_id":"9007199254740993","note":"Exact ID"}`, http.StatusOK)
	require.Equal(t, "Exact ID", f.client.UserSubscribe.GetX(ctx, sub.ID).Note)
	require.Equal(t, roundedPlan.Name, f.client.Subscribe.GetX(ctx, roundedPlan.ID).Name)
	untouched := f.client.UserSubscribe.GetX(ctx, neighbor.ID)
	require.Equal(t, neighbor.Note, untouched.Note)
	require.Equal(t, neighbor.Token, untouched.Token)
	require.Equal(t, neighbor.Download, untouched.Download)
	require.Equal(t, neighbor.Status, untouched.Status)
}

func TestCatalogReadersRecoverFromIncompletePages(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	member, session := f.account(false)
	ad := f.client.Ads.Create().SetTitle("Maintenance notice").SaveX(ctx)
	announcement := f.client.Announcement.Create().SetTitle("Available upgrades").SetShow(true).SaveX(ctx)
	doc := f.client.Document.Create().SetTitle("Setup guide").SetShow(true).SaveX(ctx)
	plan := f.client.Subscribe.Create().SetName("Business plan").SaveX(ctx)
	group := f.client.SubscribeGroup.Create().SetName("Annual plans").SaveX(ctx)
	coupon := f.client.Coupon.Create().SetName("Renewal voucher").SetCode("LOYAL").SaveX(ctx)
	code := f.client.RedemptionCode.Create().SetCode("TRIAL").SetSubscribePlan(plan.ID).SaveX(ctx)
	method := f.client.Payment.Create().SetName("Credit card").SetPlatform("Stripe").SetConfig(`{}`).SaveX(ctx)
	ticket := f.client.Ticket.Create().SetTitle("Request receipt").SetUserID(member.ID).SaveX(ctx)
	withdrawal := f.client.UserWithdrawal.Create().SetUserID(member.ID).SetAmount(450).SetContent("February commission").SaveX(ctx)
	var entity, unavailable string
	f.client.Intercept(framework.InterceptFunc(func(next framework.Querier) framework.Querier {
		return framework.QuerierFunc(func(ctx context.Context, q framework.Query) (framework.Value, error) {
			query := framework.QueryFromContext(ctx)
			if query.Type == entity && query.Op == unavailable {
				return nil, errors.New("catalog temporarily unavailable")
			}
			return next.Query(ctx, q)
		})
	}))
	for _, tc := range []struct {
		path, entity, field, value string
		id                         int64
	}{
		{"admin/ads/list", ent.TypeAds, "title", ad.Title, ad.ID},
		{"admin/announcement/list", ent.TypeAnnouncement, "title", announcement.Title, announcement.ID},
		{"public/announcement/list", ent.TypeAnnouncement, "title", announcement.Title, announcement.ID},
		{"admin/document/list", ent.TypeDocument, "title", doc.Title, doc.ID},
		{"admin/subscribe/list", ent.TypeSubscribe, "name", plan.Name, plan.ID},
		{"admin/subscribe/group/list", ent.TypeSubscribeGroup, "name", group.Name, group.ID},
		{"admin/coupon/list", ent.TypeCoupon, "code", coupon.Code, coupon.ID},
		{"admin/redemption/code/list", ent.TypeRedemptionCode, "code", code.Code, code.ID},
		{"admin/payment/list", ent.TypePayment, "name", method.Name, method.ID},
		{"admin/ticket/list", ent.TypeTicket, "title", ticket.Title, ticket.ID},
		{"public/ticket/list", ent.TypeTicket, "title", ticket.Title, ticket.ID},
		{"admin/withdrawal/list", ent.TypeUserWithdrawal, "content", withdrawal.Content, withdrawal.ID},
	} {
		t.Run(tc.path, func(t *testing.T) {
			fixture := *f
			fixture.t = t
			token := operator
			if strings.HasPrefix(tc.path, "public/") {
				token = session
			}
			base := "/v1/" + tc.path
			if tc.entity != ent.TypeSubscribeGroup { // Plan groups are returned as one unpaginated list.
				for _, query := range []string{"?page=1.5&size=10", "?page=1&size=9223372036854775808"} {
					require.Empty(t, fixture.request(http.MethodGet, base+query, token, "", xerr.InvalidParams))
				}
			}
			path := base + "?page=1&size=10"
			entity = tc.entity
			for _, unavailable = range []string{framework.OpQueryCount, framework.OpQueryAll} {
				require.Empty(t, fixture.request(http.MethodGet, path, token, "", xerr.DatabaseQueryError), unavailable)
			}
			unavailable = ""
			var page struct {
				Total         int                          `json:"total"`
				Rows          []map[string]json.RawMessage `json:"list"`
				Announcements []map[string]json.RawMessage `json:"announcements"`
			}
			require.NoError(t, json.Unmarshal(fixture.request(http.MethodGet, path, token, "", http.StatusOK), &page))
			require.Equal(t, 1, page.Total)
			rows := page.Rows
			if tc.path == "public/announcement/list" {
				rows = page.Announcements
			}
			require.Len(t, rows, 1)
			var id json.Number
			require.NoError(t, json.Unmarshal(rows[0]["id"], &id))
			require.Equal(t, fmt.Sprint(tc.id), id.String())
			require.JSONEq(t, fmt.Sprintf("%q", tc.value), string(rows[0][tc.field]))
		})
	}
}

func TestIncompleteEditorFormsReportTheFieldBeforeWriting(t *testing.T) {
	f := newRouteFixture(t)
	_, operator := f.account(true)
	_, member := f.account(false)
	writes := 0
	f.client.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			writes++
			return next.Mutate(ctx, m)
		})
	})
	for _, tc := range []struct{ verb, path, body, field string }{
		{"POST", "admin/announcement/", `{"title":"Maintenance"}`, "Content"},
		{"POST", "admin/document/", `{"title":"Getting started"}`, "Content"},
		{"PUT", "admin/document/", `{"id":"1","title":"Revised"}`, "Content"},
		{"POST", "admin/group/node", `{"sort":1}`, "Name"},
		{"POST", "admin/group/recalculate", `{"config":{}}`, "Mode"},
		{"POST", "admin/subscribe/", `{"unit_price":200}`, "Name"},
		{"PUT", "admin/subscribe/", `{"id":"1","unit_price":200}`, "Name"},
		{"POST", "admin/subscribe/group", `{"description":"Monthly"}`, "Name"},
		{"PUT", "admin/subscribe/group", `{"id":"1","description":"Monthly"}`, "Name"},
		{"POST", "admin/coupon/", `{"name":"Renewal","type":1,"discount":10,"start_time":1000}`, "ExpireTime"},
		{"PUT", "admin/coupon/", `{"id":"1","name":"Renewal","type":1,"discount":10,"start_time":1000}`, "ExpireTime"},
		{"POST", "admin/redemption/code", `{"subscribe_plan":"1","total_count":1,"quantity":1,"unit_time":"fortnight","batch_count":1}`, "UnitTime"},
		{"PUT", "admin/redemption/code/status", `{"id":"1","status":3}`, "Status"},
		{"POST", "admin/payment/", `{"name":"Card","platform":"Stripe","enable":true}`, "Config"},
		{"PUT", "admin/payment/", `{"id":"1","name":"Card","platform":"Stripe","config":{}}`, "Enable"},
		{"POST", "admin/order/", `{"user_id":"1","type":1,"price":300,"amount":300,"quantity":1001}`, "Quantity"},
		{"PUT", "admin/order/status", `{"id":"1","trade_no":"receipt"}`, "Status"},
		{"PUT", "admin/withdrawal/status", `{"id":"1","status":3}`, "Status"},
		{"PUT", "admin/user/basic", `{"balance":250}`, "UserId"},
		{"PUT", "admin/user/notify", `{"enable_login_notify":true}`, "UserId"},
		{"PUT", "admin/ticket/", `{"id":"1"}`, "Status"},
		{"POST", "admin/ticket/follow", `{"ticket_id":"1","from":"operator","content":"Receipt","type":3}`, "Type"},
		{"POST", "public/ticket/", `{"description":"Receipt requested"}`, "Title"},
		{"PUT", "public/ticket/", `{"id":"1"}`, "Status"},
		{"POST", "public/ticket/follow", `{"ticket_id":"1","content":"Receipt","type":3}`, "Type"},
		{"PUT", "public/user/bind_email", `{"email":"member@example.test"}`, "Code"},
		{"PUT", "public/user/bind_mobile", `{"mobile":"2025550101","area_code":"+1"}`, "Code"},
		{"POST", "public/user/verify_email", `{"email":"member@example.test"}`, "Code"},
		{"PUT", "public/user/password", `{"old_password":"old"}`, "Password"},
		{"PUT", "public/user/subscribe_note", fmt.Sprintf(`{"user_subscribe_id":"1","note":%q}`, strings.Repeat("界", 501)), "Note"},
		{"PUT", "public/user/rules", `{}`, "Rules"},
		{"POST", "auth/login", `{"email":"member@example.test"}`, "Password"},
		{"POST", "auth/admin/login", `{"email":"operator@example.test"}`, "Password"},
		{"POST", "auth/register", `{"email":"member@example.test"}`, "Password"},
		{"POST", "auth/reset", `{"email":"member@example.test","code":"fixture"}`, "Password"},
		{"POST", "auth/admin/reset", `{"email":"operator@example.test","code":"fixture"}`, "Password"},
		{"POST", "auth/login/device", `{"identifier":"laptop"}`, "UserAgent"},
		{"POST", "auth/login/telephone", `{"telephone":"2025550101"}`, "TelephoneAreaCode"},
		{"POST", "auth/register/telephone", `{"telephone":"2025550101","password":"fixture"}`, "TelephoneAreaCode"},
		{"POST", "auth/reset/telephone", `{"telephone":"2025550101","password":"fixture"}`, "TelephoneAreaCode"},
		{"POST", "auth/oauth/login/token", `{"method":"google"}`, "Callback"},
		{"POST", "auth/oauth/register", `{"invite":"MEMBER"}`, "OAuthToken"},
		{"POST", "common/check_verification_code", `{"account":"member@example.test","code":"123456","method":"postal","type":1}`, "Method"},
		{"POST", "common/send_sms_code", `{"type":1,"telephone":"2025550101"}`, "TelephoneAreaCode"},
		{"POST", "common/send_code", `{"email":"member@example.test"}`, "Type"},
		{"POST", "auth/oauth/login", `{"redirect":"https://panel.example.test/return"}`, "Method"},
		{"POST", "public/order/pre", `{"subscribe_id":"1","quantity":1001}`, "Quantity"},
		{"POST", "public/order/purchase", `{"subscribe_id":"1","quantity":-1}`, "Quantity"},
		{"POST", "public/order/renewal", `{"user_subscribe_id":"1","quantity":1001}`, "Quantity"},
		{"POST", "public/order/recharge", `{"payment":"1","amount":2000000001}`, "Amount"},
		{"POST", "public/order/close", `{"orderNo":""}`, "OrderNo"},
		{"POST", "public/redemption/", `{"code":""}`, "Code"},
		{"PUT", "public/user/unbind_device", `{"id":"0"}`, "Id"},
		{"POST", "admin/server/node/create", `{"name":"Regional relay","sort":-1}`, "Sort"},
		{"POST", "admin/server/node/update", `{"id":"1","sort":-1}`, "Sort"},
		{"POST", "admin/server/update", `{"id":"1","sort":-1}`, "Sort"},
		{"POST", "admin/server/node_config/update", `{"server_id":0}`, "ServerID"},
		{"PUT", "admin/group/node", `{"id":"1","sort":-1}`, "Sort"},
		{"DELETE", "admin/group/node", `{"name":"Retiring"}`, "Id"},
		{"DELETE", "admin/subscribe/", `{"name":"Retiring"}`, "Id"},
		{"DELETE", "admin/subscribe/group", `{"name":"Retiring"}`, "Id"},
		{"DELETE", "admin/document/", `{"title":"Withdrawn"}`, "Id"},
		{"DELETE", "admin/announcement/", `{"title":"Withdrawn"}`, "Id"},
		{"DELETE", "admin/coupon/", `{"code":"RETIRED"}`, "Id"},
		{"DELETE", "admin/redemption/code", `{"code":"RETIRED"}`, "Id"},
		{"DELETE", "admin/payment/", `{"name":"Retiring"}`, "Id"},
		{"DELETE", "admin/user/", `{"email":"archived@example.test"}`, "Id"},
	} {
		t.Run(tc.verb+" "+tc.path, func(t *testing.T) {
			session := ""
			if strings.HasPrefix(tc.path, "admin/") {
				session = operator
			} else if strings.HasPrefix(tc.path, "public/") {
				session = member
			}
			for _, body := range []string{tc.body, strings.TrimSuffix(tc.body, "}")} {
				req := httptest.NewRequestWithContext(t.Context(), tc.verb, "/v1/"+tc.path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", session)
				rec := httptest.NewRecorder()
				f.router.ServeHTTP(rec, req)
				var reply struct {
					Code    uint32          `json:"code"`
					Message string          `json:"msg"`
					Data    json.RawMessage `json:"data"`
				}
				require.Equal(t, http.StatusOK, rec.Code)
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &reply))
				require.Equal(t, xerr.InvalidParams, reply.Code, reply.Message)
				require.Empty(t, reply.Data)
				if body == tc.body {
					require.Contains(t, strings.ToLower(reply.Message), strings.ToLower(tc.field))
				}
				require.Zero(t, writes, "invalid form caused a persistent change")
			}
		})
	}
}

func TestDetailLinksRequireTheSelectedRecord(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	owner, member := f.account(false)
	ticket := f.client.Ticket.Create().SetUserID(owner.ID).SetTitle("Billing receipt").SaveX(ctx)
	article := f.client.Document.Create().SetTitle("Export a receipt").SetShow(true).SaveX(ctx)
	announcement := f.client.Announcement.Create().SetTitle("Receipt export available").SetShow(true).SaveX(ctx)
	plan := f.client.Subscribe.Create().SetName("Monthly pass").SaveX(ctx)
	sub := f.client.UserSubscribe.Create().SetOrderID(1).SetSubscribeID(plan.ID).SetUserID(owner.ID).SetStatus(1).SaveX(ctx)
	for _, tc := range []struct {
		path, session string
		id            int64
	}{
		{"/v1/admin/user/detail", operator, owner.ID},
		{"/v1/admin/announcement/detail", operator, announcement.ID},
		{"/v1/admin/document/detail", operator, article.ID},
		{"/v1/public/document/detail", member, article.ID},
		{"/v1/admin/ticket/detail", operator, ticket.ID},
		{"/v1/public/ticket/detail", member, ticket.ID},
		{"/v1/admin/subscribe/details", operator, plan.ID},
		{"/v1/admin/user/subscribe/detail", operator, sub.ID},
	} {
		t.Run(tc.path, func(t *testing.T) {
			for _, query := range []string{"", "?id=1.5", "?id=9223372036854775808"} {
				require.Empty(t, f.request(http.MethodGet, tc.path+query, tc.session, "", xerr.InvalidParams))
			}
			var selected struct {
				ID json.Number `json:"id"`
			}
			require.NoError(t, json.Unmarshal(f.request(http.MethodGet, fmt.Sprintf("%s?id=%d", tc.path, tc.id), tc.session, "", http.StatusOK), &selected))
			require.Equal(t, fmt.Sprint(tc.id), selected.ID.String())
		})
	}
}

func TestAccountAndCampaignReportFiltersRejectInvalidSelections(t *testing.T) {
	f := newRouteFixture(t)
	_, operator := f.account(true)
	_, member := f.account(false)
	for _, tc := range []struct{ path, filter string }{
		{"admin/user/list", "user_id"}, {"admin/user/auth_method", "user_id"},
		{"admin/user/subscribe", "user_id"}, {"admin/user/login/logs", "user_id"},
		{"admin/user/subscribe/logs", "user_id"}, {"admin/user/subscribe/device", "user_id"},
		{"admin/user/subscribe/reset/logs", "user_subscribe_id"}, {"admin/user/subscribe/traffic_logs", "subscribe_id"},
		{"admin/redemption/record/list", "code_id"}, {"admin/order/list", "user_id"},
		{"admin/server/node/list", "node_group_id"}, {"admin/group/preview", "user_id"},
		{"admin/group/history/detail", "id"}, {"admin/server/node_config", "server_id"},
		{"admin/server/protocols", "id"}, {"admin/ads/detail", "id"},
		{"admin/marketing/quota/list", "status"}, {"admin/marketing/email/batch/list", "scope"},
		{"admin/log/message/list", "type"}, {"admin/withdrawal/list", "status"},
		{"public/order/list", "page"}, {"public/user/affiliate/list", "size"},
		{"public/user/commission_log", "size"}, {"public/user/withdrawal_log", "size"},
		{"public/user/subscribe_log", "size"}, {"public/user/traffic_stats", "days"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			session := operator
			if strings.HasPrefix(tc.path, "public/") {
				session = member
			}
			params := url.Values{"page": {"1"}, "size": {"10"}}
			params.Set(tc.filter, "1.5")
			require.Empty(t, f.request(http.MethodGet, "/v1/"+tc.path+"?"+params.Encode(), session, "", xerr.InvalidParams))
		})
	}
}

func TestInvalidReportFiltersReturnParameterErrorsInsteadOfUnfilteredData(t *testing.T) {
	f := newRouteFixture(t)
	_, operator := f.account(true)
	const base = "/v1/admin/log/"
	for _, path := range []string{"balance/list", "commission/list", "email/list", "gift/list", "login/list", "mobile/list", "order/list", "register/list", "server/traffic/list", "subscribe/list", "subscribe/reset/list", "subscribe/traffic/list", "traffic/details"} {
		t.Run(path, func(t *testing.T) {
			for _, query := range []string{"page=9223372036854775808&size=10", "page=1&size=10&end_date=2025-02-29"} {
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, base+path+"?"+query, nil)
				req.Header.Set("Authorization", operator)
				rec := httptest.NewRecorder()
				f.router.ServeHTTP(rec, req)
				var reply struct {
					Code    uint32          `json:"code"`
					Message string          `json:"msg"`
					Data    json.RawMessage `json:"data"`
				}
				require.Equal(t, http.StatusOK, rec.Code)
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &reply))
				require.Equal(t, xerr.InvalidParams, reply.Code)
				field := "page"
				if strings.Contains(query, "end_date") {
					field = "enddate"
				}
				require.Contains(t, strings.ToLower(reply.Message), field)
				require.Empty(t, reply.Data)
			}
		})
	}
}
