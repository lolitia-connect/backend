package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/network/standard"
	"github.com/gorilla/websocket"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/aes"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestMemberDeviceDisconnectAndUnbind(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	other, otherSession := f.account(false)
	f.client.UserDevice.Create().SetUserID(other.ID).SetIdentifier("other-laptop").SetIP("192.0.2.10").SaveX(ctx)
	f.service.Config.JwtAuth.AccessExpire = 300
	f.service.DeviceManager = svc.NewDeviceManager(f.service)
	t.Cleanup(f.service.DeviceManager.Close)
	const address = "device.owner@example.test"
	const password = "member-device-fixture"
	const identifier = "member-desktop"
	const admin = "/v1/admin/user"
	const member = "/v1/public/user"
	f.request(http.MethodPost, admin+"/", operator, fmt.Sprintf(`{"email":%q,"password":%q}`, address, password), http.StatusOK)
	login := func(id, agent string) string {
		t.Helper()
		body := fmt.Sprintf(`{"identifier":%q,"email":%q,"password":%q}`, id, address, password)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", agent)
		var result types.LoginResponse
		require.NoError(t, json.Unmarshal(f.serve(req, http.StatusOK, http.StatusOK), &result))
		require.NotEmpty(t, result.Token)
		return result.Token
	}
	devices := func(session string) []types.UserDevice {
		t.Helper()
		var result types.GetDeviceListResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, member+"/devices", session, "", http.StatusOK), &result))
		require.EqualValues(t, len(result.List), result.Total)
		return result.List
	}
	web := login("", "member-browser")
	login(identifier, "desktop/1")
	bound := devices(web)
	require.Len(t, bound, 1)
	id := bound[0].Id
	desktop := login(identifier, "desktop/2")
	bound = devices(web)
	require.Len(t, bound, 1)
	require.Equal(t, id, bound[0].Id)
	require.Equal(t, "desktop/2", bound[0].UserAgent)
	var profile types.User
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, member+"/info", web, "", http.StatusOK), &profile))

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	engine := server.New(server.WithListener(listener), server.WithTransport(standard.NewTransporter), server.WithDisablePrintRoute(true))
	RegisterNativeHandlers(engine, f.service)
	stopped := make(chan error, 1)
	go func() { stopped <- engine.Run() }()
	const timeout = 5 * time.Second
	t.Cleanup(func() {
		shutdown, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := engine.Shutdown(shutdown); err != nil {
			t.Error(err)
			_ = listener.Close()
		}
		select {
		case err := <-stopped:
			if err != nil {
				t.Error(err)
			}
		case <-shutdown.Done():
			t.Error("device server did not stop")
		}
	})
	dialer := websocket.Dialer{HandshakeTimeout: timeout}
	url := fmt.Sprintf("ws://%s/v1/app/ws/%d/%s", listener.Addr(), profile.Id, identifier)
	conn, _, err := dialer.DialContext(ctx, url, http.Header{"Authorization": {desktop}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.Eventually(t, func() bool {
		row, err := f.client.UserDevice.Get(ctx, id)
		return err == nil && row.Online
	}, timeout, 10*time.Millisecond)
	var operatorView types.GetUserSubscribeDevicesResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, fmt.Sprintf("%s/subscribe/device?user_id=%d&page=1&size=5", admin, profile.Id), operator, "", http.StatusOK), &operatorView))
	require.Equal(t, devices(web), operatorView.List)
	require.True(t, operatorView.List[0].Online)
	ref := fmt.Sprintf(`{"id":"%d"}`, id)
	f.request(http.MethodPut, admin+"/device/kick_offline", operator, ref, http.StatusOK)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(timeout)))
	var message svc.DeviceMessage
	require.NoError(t, conn.ReadJSON(&message))
	require.Equal(t, svc.DeviceKickedAdmin, message.Method)
	f.request(http.MethodGet, member+"/devices", desktop, "", xerr.InvalidAccess)
	bound = devices(web)
	require.Len(t, bound, 1)
	require.False(t, bound[0].Online)
	require.True(t, bound[0].Enabled)
	require.Eventually(t, func() bool { return f.client.UserDeviceOnlineRecord.Query().CountX(ctx) == 1 }, timeout, 10*time.Millisecond)
	record := f.client.UserDeviceOnlineRecord.Query().OnlyX(ctx)
	require.Equal(t, profile.Id, record.UserID)
	require.Equal(t, identifier, record.Identifier)
	f.request(http.MethodPut, member+"/unbind_device", web, ref, http.StatusOK)
	require.Empty(t, devices(web))
	others := devices(otherSession)
	require.Len(t, others, 1)
	require.Equal(t, "other-laptop", others[0].Identifier)
	var methods types.GetOAuthMethodsResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, member+"/oauth_methods", login("", "member-browser"), "", http.StatusOK), &methods))
	require.Len(t, methods.Methods, 1)
	require.Equal(t, address, methods.Methods[0].AuthIdentifier)
}

func TestDeviceClientCanLookUpAccountAndVerifySecurityCode(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	const secret = "device-client-fixture"
	const address = "native@example.test"
	f.service.Config.Device.Enable = true
	f.service.Config.Device.SecuritySecret = secret
	owner, _ := f.account(false)
	f.client.UserAuthMethod.Create().SetUserID(owner.ID).SetAuthType("email").SetAuthIdentifier(address).SaveX(ctx)
	key := fmt.Sprintf("%s:%s:%s", config.AuthCodeCacheKey, constant.Security, address)
	require.NoError(t, f.service.Redis.Set(ctx, key, `{"code":"384175"}`, time.Minute).Err())

	request := func(method, path, payload string, target any) {
		t.Helper()
		ciphertext, nonce, err := pkgaes.Encrypt([]byte(payload), secret)
		require.NoError(t, err)
		body := ""
		if method == http.MethodGet {
			path += "?" + url.Values{"data": {ciphertext}, "time": {nonce}}.Encode()
		} else {
			body = fmt.Sprintf(`{"data":%q,"time":%q}`, ciphertext, nonce)
		}
		req := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
		req.Header.Set("Login-Type", "device")
		req.Header.Set("Content-Type", "application/json")
		var encrypted struct {
			Data string `json:"data"`
			Time string `json:"time"`
		}
		require.NoError(t, json.Unmarshal(f.serve(req, http.StatusOK, http.StatusOK), &encrypted))
		require.NotEmpty(t, encrypted.Data)
		require.NotEmpty(t, encrypted.Time)
		plain, err := pkgaes.Decrypt(encrypted.Data, secret, encrypted.Time)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal([]byte(plain), target))
	}
	var account types.CheckUserResponse
	request(http.MethodGet, "/v1/auth/check", fmt.Sprintf(`{"email":%q}`, address), &account)
	require.True(t, account.Exist)
	for _, tc := range []struct {
		code string
		ok   bool
	}{{"999999", false}, {"384175", true}} {
		var verification types.CheckVerificationCodeRespone
		request(http.MethodPost, "/v1/common/check_verification_code", fmt.Sprintf(`{"method":"email","account":%q,"type":%d,"code":%q}`, address, constant.Security, tc.code), &verification)
		require.Equal(t, tc.ok, verification.Status)
	}
}
