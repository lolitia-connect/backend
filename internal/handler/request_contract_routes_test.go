package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
			for _, body := range []string{`{}`, fmt.Sprintf(`{"ids":["%d",false]}`, target.id), fmt.Sprintf(`{"ids":["%d"`, target.id)} {
				require.Empty(t, f.request(http.MethodDelete, path, operator, body, xerr.InvalidParams))
				require.True(t, target.retained(), "invalid selection changed a selected record")
			}
		})
	}
	f.request(http.MethodGet, "/v1/public/user/info", session, "", http.StatusOK)
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
