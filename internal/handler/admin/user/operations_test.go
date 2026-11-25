package user

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/gorilla/websocket"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/ent/userauthmethod"
	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/device"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func adminUserHTTPFixture(t *testing.T) (*svc.ServiceContext, *ent.Client) {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	return &svc.ServiceContext{Store: repository.NewStore(client, nil)}, client
}

func adminUserRequest(t *testing.T, router http.Handler, method, path, body string, wantCode uint32, dst any) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, "%s %s: %s", method, path, recorder.Body.String())
	var response struct {
		Code uint32          `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, wantCode, response.Code, "%s %s: %s", method, path, recorder.Body.String())
	if dst != nil {
		require.NoError(t, json.Unmarshal(response.Data, dst))
	}
}

func TestAdminAuthMethodHTTPUpdateAndRemoval(t *testing.T) {
	service, client := adminUserHTTPFixture(t)
	ctx := t.Context()
	account := client.User.Create().SetPassword("fixture").SaveX(ctx)
	other := client.User.Create().SetPassword("fixture").SaveX(ctx)
	email := client.UserAuthMethod.Create().SetUserID(account.ID).SetAuthType("email").SetAuthIdentifier("old@example.test").SaveX(ctx)
	mobile := client.UserAuthMethod.Create().SetUserID(account.ID).SetAuthType("mobile").SetAuthIdentifier("+12025550123").SaveX(ctx)
	occupied := client.UserAuthMethod.Create().SetUserID(other.ID).SetAuthType("email").SetAuthIdentifier("occupied@example.test").SetVerified(true).SaveX(ctx)

	const path = "/v1/admin/user/auth_method"
	router := hertzx.New()
	router.GET(path, GetUserAuthMethodHandler(service))
	router.POST(path, CreateUserAuthMethodHandler(service))
	router.PUT(path, UpdateUserAuthMethodHandler(service))
	router.DELETE(path, DeleteUserAuthMethodHandler(service))

	update := func(identifier string, wantCode uint32) {
		t.Helper()
		body := fmt.Sprintf(`{"user_id":"%d","auth_type":"email","auth_identifier":%q}`, account.ID, identifier)
		adminUserRequest(t, router, http.MethodPut, path, body, wantCode, nil)
	}
	update("OCCUPIED@example.test", xerr.DatabaseUpdateError)
	unchanged := client.UserAuthMethod.GetX(ctx, email.ID)
	require.Equal(t, email.AuthIdentifier, unchanged.AuthIdentifier)
	require.False(t, unchanged.Verified)

	update("New@EXAMPLE.test", http.StatusOK)
	stored := client.UserAuthMethod.GetX(ctx, email.ID)
	require.Equal(t, "new@example.test", stored.AuthIdentifier)
	require.True(t, stored.Verified)
	query := fmt.Sprintf("%s?user_id=%d", path, account.ID)
	var methods types.GetUserAuthMethodResponse
	adminUserRequest(t, router, http.MethodGet, query, "", http.StatusOK, &methods)
	wantMobile := types.UserAuthMethod{AuthType: "mobile", AuthIdentifier: mobile.AuthIdentifier}
	require.ElementsMatch(t, []types.UserAuthMethod{
		{AuthType: "email", AuthIdentifier: stored.AuthIdentifier, Verified: true}, wantMobile,
	}, methods.AuthMethods)

	adminUserRequest(t, router, http.MethodDelete, path, fmt.Sprintf(`{"user_id":"%d","auth_type":"email"}`, account.ID), http.StatusOK, nil)
	_, err := client.UserAuthMethod.Get(ctx, email.ID)
	require.True(t, ent.IsNotFound(err), "deleted auth method lookup returned %v", err)
	require.Equal(t, occupied.AuthIdentifier, client.UserAuthMethod.GetX(ctx, occupied.ID).AuthIdentifier)
	require.Equal(t, 1, client.UserAuthMethod.Query().Where(userauthmethod.UserID(account.ID)).CountX(ctx))
	adminUserRequest(t, router, http.MethodGet, query, "", http.StatusOK, &methods)
	require.Equal(t, []types.UserAuthMethod{wantMobile}, methods.AuthMethods)
	update("another@example.test", xerr.DatabaseQueryError)

	upsert := func(identifier string) *ent.UserAuthMethod {
		t.Helper()
		body := fmt.Sprintf(`{"user_id":"%d","auth_type":"email","auth_identifier":%q}`, account.ID, identifier)
		adminUserRequest(t, router, http.MethodPost, path, body, http.StatusOK, nil)
		return client.UserAuthMethod.Query().Where(userauthmethod.UserID(account.ID), userauthmethod.AuthType("email")).OnlyX(ctx)
	}
	restored := upsert("Restored@example.test")
	require.Equal(t, "restored@example.test", restored.AuthIdentifier)
	require.True(t, restored.Verified)
	replaced := upsert("replacement@example.test")
	require.Equal(t, restored.ID, replaced.ID)
	require.Equal(t, "replacement@example.test", replaced.AuthIdentifier)
}

func TestAdminDeviceHTTPKickDisableAndRemove(t *testing.T) {
	service, client := adminUserHTTPFixture(t)
	ctx := t.Context()
	account := client.User.Create().SetPassword("fixture").SaveX(ctx)
	other := client.User.Create().SetPassword("fixture").SaveX(ctx)
	active := client.UserDevice.Create().SetUserID(account.ID).SetIdentifier("laptop").SetUserAgent("fixture-agent").
		SetIP("192.0.2.10").SetOnline(true).SetEnabled(true).SaveX(ctx)
	unrelated := client.UserDevice.Create().SetUserID(other.ID).SetIdentifier("phone").SetIP("192.0.2.11").SetOnline(true).SetEnabled(true).SaveX(ctx)
	manager := device.NewDeviceManager(60, 30)
	t.Cleanup(manager.Close)
	service.DeviceManager = manager

	registered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		manager.AddDevice(w, r, "fixture-session", account.ID, active.Identifier, 2)
		close(registered)
	}))
	t.Cleanup(server.Close)
	ws, handshake, err := websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if handshake != nil && err != nil {
		_ = handshake.Body.Close()
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	select {
	case <-registered:
	case <-time.After(5 * time.Second):
		t.Fatal("device did not register")
	}

	const path = "/v1/admin/user/device"
	const devicesPath = "/v1/admin/user/subscribe/device"
	router := hertzx.New()
	router.PUT(path+"/kick_offline", KickOfflineByUserDeviceHandler(service))
	router.PUT(path, UpdateUserDeviceHandler(service))
	router.DELETE(path, DeleteUserDeviceHandler(service))
	router.GET(devicesPath, GetUserSubscribeDevicesHandler(service))
	id := fmt.Sprintf(`{"id":"%d"}`, active.ID)

	adminUserRequest(t, router, http.MethodPut, path+"/kick_offline", id, http.StatusOK, nil)
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, _, err = ws.ReadMessage()
	var disconnected *websocket.CloseError
	require.ErrorAs(t, err, &disconnected, "kick must close the registered connection")
	kicked := client.UserDevice.GetX(ctx, active.ID)
	require.False(t, kicked.Online)
	require.True(t, kicked.Enabled, "kicking does not disable the device")

	adminUserRequest(t, router, http.MethodPut, path, fmt.Sprintf(`{"id":"%d","enabled":false}`, active.ID), http.StatusOK, nil)
	require.False(t, client.UserDevice.GetX(ctx, active.ID).Enabled)
	query := fmt.Sprintf("%s?user_id=%d&page=1&size=10", devicesPath, account.ID)
	var devices types.GetUserSubscribeDevicesResponse
	adminUserRequest(t, router, http.MethodGet, query, "", http.StatusOK, &devices)
	require.EqualValues(t, 1, devices.Total)
	require.Len(t, devices.List, 1)
	listed := devices.List[0]
	require.Equal(t, active.ID, listed.Id)
	require.Equal(t, active.Identifier, listed.Identifier)
	require.Equal(t, "fixture-agent", listed.UserAgent)
	require.Equal(t, active.IP, listed.Ip)
	require.False(t, listed.Enabled)
	require.False(t, listed.Online)

	adminUserRequest(t, router, http.MethodDelete, path, id, http.StatusOK, nil)
	_, err = client.UserDevice.Get(ctx, active.ID)
	require.True(t, ent.IsNotFound(err), "deleted device lookup returned %v", err)
	require.True(t, client.UserDevice.GetX(ctx, unrelated.ID).Online)
	adminUserRequest(t, router, http.MethodPut, path+"/kick_offline", id, xerr.DatabaseQueryError, nil)
}

func TestAdminSubscriptionHTTPResetAndQueryDetails(t *testing.T) {
	service, client := adminUserHTTPFixture(t)
	ctx := t.Context()
	account := client.User.Create().SetPassword("fixture").SetReferCode("details-fixture").SaveX(ctx)
	plan := client.Subscribe.Create().SetName("Monthly").SaveX(ctx)
	expires := time.Date(2035, 2, 3, 4, 5, 6, 0, time.UTC)
	sub := client.UserSubscribe.Create().SetUserID(account.ID).SetSubscribeID(plan.ID).SetOrderID(1).
		SetToken("original-token").SetUUID("00000000-0000-4000-8000-000000000001").
		SetExpireTime(expires).SetGroupLocked(true).SetTraffic(4096).SetUpload(120).SetDownload(340).
		SetExpiredUpload(11).SetExpiredDownload(22).SetStatus(5).SaveX(ctx)

	const path = "/v1/admin/user/subscribe"
	router := hertzx.New()
	router.GET(path+"/detail", GetUserSubscribeByIdHandler(service))
	router.POST(path+"/reset/token", ResetUserSubscribeTokenHandler(service))
	router.POST(path+"/reset/traffic", ResetUserSubscribeTrafficHandler(service))
	router.DELETE(path, DeleteUserSubscribeHandler(service))
	query := fmt.Sprintf("%s/detail?id=%d", path, sub.ID)
	id := fmt.Sprintf(`{"user_subscribe_id":"%d"}`, sub.ID)

	var details types.UserSubscribeDetail
	adminUserRequest(t, router, http.MethodGet, path+"/detail", "", xerr.InvalidParams, nil)
	adminUserRequest(t, router, http.MethodGet, query, "", http.StatusOK, &details)
	require.Equal(t, sub.ID, details.Id)
	require.Equal(t, account.ID, details.User.Id)
	require.Equal(t, account.ReferCode, details.User.ReferCode)
	require.Equal(t, plan.ID, details.Subscribe.Id)
	require.Equal(t, plan.Name, details.Subscribe.Name)
	require.Equal(t, sub.Download, details.Download)
	require.True(t, details.GroupLocked)

	adminUserRequest(t, router, http.MethodPost, path+"/reset/token", id, http.StatusOK, nil)
	rotated := client.UserSubscribe.GetX(ctx, sub.ID)
	require.NotEmpty(t, rotated.Token)
	require.NotEqual(t, sub.Token, rotated.Token)
	require.NotEmpty(t, rotated.UUID)
	require.NotEqual(t, sub.UUID, rotated.UUID)
	require.Equal(t, sub.Upload, rotated.Upload)
	require.Equal(t, sub.Download, rotated.Download)
	_, err := service.Store.User().FindOneSubscribeByToken(ctx, sub.Token)
	require.True(t, ent.IsNotFound(err), "old credential lookup returned %v", err)

	adminUserRequest(t, router, http.MethodPost, path+"/reset/traffic", id, http.StatusOK, nil)
	reset := client.UserSubscribe.GetX(ctx, sub.ID)
	require.Zero(t, reset.Upload)
	require.Zero(t, reset.Download)
	require.Equal(t, sub.ExpiredUpload, reset.ExpiredUpload)
	require.Equal(t, sub.ExpiredDownload, reset.ExpiredDownload)
	require.Equal(t, sub.Traffic, reset.Traffic)
	require.Equal(t, sub.Status, reset.Status)
	require.True(t, expires.Equal(reset.ExpireTime))
	require.Equal(t, rotated.UUID, reset.UUID)
	adminUserRequest(t, router, http.MethodGet, query, "", http.StatusOK, &details)
	require.Equal(t, rotated.Token, details.Token)
	require.Zero(t, details.Upload)
	require.Zero(t, details.Download)

	adminUserRequest(t, router, http.MethodDelete, path, id, http.StatusOK, nil)
	adminUserRequest(t, router, http.MethodGet, query, "", xerr.DatabaseQueryError, nil)
	_, err = service.Store.User().FindOneSubscribeByToken(ctx, rotated.Token)
	require.True(t, ent.IsNotFound(err), "removed credential lookup returned %v", err)
	require.Equal(t, plan.Name, client.Subscribe.GetX(ctx, plan.ID).Name)
}

func TestAdminAuditHTTPQueriesSkipBadPayloadsAndScopeResults(t *testing.T) {
	service, client := adminUserHTTPFixture(t)
	ctx := t.Context()
	account := client.User.Create().SetPassword("fixture").SaveX(ctx)
	plan := client.Subscribe.Create().SetName("Monthly").SaveX(ctx)
	sub := client.UserSubscribe.Create().SetUserID(account.ID).SetSubscribeID(plan.ID).SetOrderID(1).SaveX(ctx)
	created := time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC)
	logEntry := func(kind modellog.Type, id int64, content string) *ent.SystemLog {
		return client.SystemLog.Create().SetType(kind.Uint8()).SetObjectID(id).
			SetDate(created.Format(time.DateOnly)).SetCreatedAt(created).SetContent(content).SaveX(ctx)
	}
	const loginPayload = `{"login_ip":"192.0.2.20","user_agent":"fixture-client","success":true}`
	resetAt := created.Add(-time.Minute).UnixMilli()
	resetPayload := fmt.Sprintf(`{"type":1,"order_no":"reset-order","timestamp":%d}`, resetAt)
	login := logEntry(modellog.TypeLogin, account.ID, loginPayload)
	reset := logEntry(modellog.TypeResetSubscribe, sub.ID, resetPayload)
	logEntry(modellog.TypeLogin, account.ID, "{")
	logEntry(modellog.TypeResetSubscribe, sub.ID, "{")
	logEntry(modellog.TypeLogin, account.ID+1, loginPayload)
	logEntry(modellog.TypeResetSubscribe, sub.ID+1, resetPayload)

	const loginPath = "/v1/admin/user/login/logs"
	const resetPath = "/v1/admin/user/subscribe/reset/logs"
	router := hertzx.New()
	router.GET(loginPath, GetUserLoginLogsHandler(service))
	router.GET(resetPath, GetUserSubscribeResetTrafficLogsHandler(service))

	var loginLogs types.GetUserLoginLogsResponse
	adminUserRequest(t, router, http.MethodGet, fmt.Sprintf("%s?user_id=%d&page=1&size=10", loginPath, account.ID), "", http.StatusOK, &loginLogs)
	require.EqualValues(t, 2, loginLogs.Total)
	require.Equal(t, []types.UserLoginLog{{
		Id: login.ID, UserId: account.ID, LoginIP: "192.0.2.20", UserAgent: "fixture-client", Success: true, Timestamp: created.UnixMilli(),
	}}, loginLogs.List)

	var resetLogs types.GetUserSubscribeResetTrafficLogsResponse
	adminUserRequest(t, router, http.MethodGet, fmt.Sprintf("%s?user_subscribe_id=%d&page=1&size=10", resetPath, sub.ID), "", http.StatusOK, &resetLogs)
	require.EqualValues(t, 2, resetLogs.Total)
	require.Equal(t, []types.ResetSubscribeTrafficLog{{
		Id: reset.ID, UserSubscribeId: sub.ID, Type: 1, OrderNo: "reset-order", Timestamp: resetAt,
	}}, resetLogs.List)
}
