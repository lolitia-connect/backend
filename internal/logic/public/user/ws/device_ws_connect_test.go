package ws

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/network/standard"
	"github.com/cloudwego/hertz/pkg/route/param"
	"github.com/gorilla/websocket"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/ent/userdevice"
	"github.com/perfect-panel/server/ent/userdeviceonlinerecord"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/usersession"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newDeviceDatabase(t *testing.T) *ent.Client {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestDeviceWebSocketRejectsInvalidRequestsBeforeWritingDevices(t *testing.T) {
	ctx := t.Context()
	client := newDeviceDatabase(t)
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil)}
	owner := client.User.Create().SetPassword("unused").SaveX(ctx)
	other := client.User.Create().SetPassword("unused").SaveX(ctx)
	foreign := client.UserDevice.Create().SetUserID(other.ID).SetIP("").SetIdentifier("foreign").SaveX(ctx)
	disabled := client.UserDevice.Create().SetUserID(owner.ID).SetIP("").SetIdentifier("disabled").SetEnabled(false).SaveX(ctx)

	for _, tt := range []struct {
		name, queryID, contextID, pathUserID, session string
		want                                          uint32
	}{
		{name: "missing identifier", session: "session", want: xerr.InvalidParams},
		{name: "oversized identifier", queryID: strings.Repeat("x", 256), session: "session", want: xerr.InvalidParams},
		{name: "missing session", queryID: "new", want: xerr.InvalidAccess},
		{name: "different user in path", queryID: "new", pathUserID: strconv.FormatInt(other.ID, 10), session: "session", want: xerr.InvalidAccess},
		{name: "identifier owned by another user", queryID: "foreign", session: "session", want: xerr.InvalidAccess},
		{name: "disabled context identifier overrides query", queryID: "new", contextID: "disabled", session: "session", want: xerr.InvalidAccess},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requestCtx := context.WithValue(t.Context(), constant.CtxKeyUser, &user.User{Id: owner.ID})
			if tt.session != "" {
				requestCtx = context.WithValue(requestCtx, constant.CtxKeySessionID, tt.session)
			}
			if tt.contextID != "" {
				requestCtx = context.WithValue(requestCtx, constant.CtxKeyIdentifier, tt.contextID)
			}
			raw := app.NewContext(0)
			raw.Request.SetRequestURI("/socket?identifier=" + tt.queryID)
			raw.Params = append(raw.Params, param.Param{Key: "userid", Value: tt.pathUserID})
			err := NewDeviceWsConnectLogic(requestCtx, service).DeviceWsConnect(hertzx.NewContext(requestCtx, raw))
			var code *xerr.CodeError
			require.ErrorAs(t, err, &code)
			require.Equal(t, tt.want, code.GetErrCode())
			require.Equal(t, 2, client.UserDevice.Query().CountX(t.Context()))
			for _, original := range []*ent.UserDevice{foreign, disabled} {
				persisted := client.UserDevice.GetX(t.Context(), original.ID)
				require.Equal(t, original.UserID, persisted.UserID)
				require.Equal(t, original.Identifier, persisted.Identifier)
				require.Equal(t, original.Enabled, persisted.Enabled)
				require.False(t, persisted.Online)
				require.WithinDuration(t, original.UpdatedAt, persisted.UpdatedAt, 0)
			}
		})
	}
}

func TestDeviceWebSocketEnforcesActivePlanLimitAndRevokesEvictedSession(t *testing.T) {
	const timeout = 5 * time.Second
	ctx := t.Context()
	client := newDeviceDatabase(t)
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, cache), Redis: cache}
	service.Config.JwtAuth.AccessSecret = "device-websocket-test-secret"
	service.Config.JwtAuth.AccessExpire = 3600
	account := client.User.Create().SetPassword("unused").SaveX(ctx)
	oldest := client.UserDevice.Create().SetIdentifier("device-1").SetUserID(account.ID).SetIP("").SaveX(ctx)

	active := client.Subscribe.Create().SetDeviceLimit(4).SaveX(ctx)
	client.UserSubscribe.Create().SetUserID(account.ID).SetOrderID(1).SetSubscribeID(active.ID).
		SetStatus(2).SetExpireTime(time.UnixMilli(0)).SaveX(ctx)
	expired := client.Subscribe.Create().SetDeviceLimit(10).SaveX(ctx)
	recentlyExpired := time.Now().Add(-time.Hour)
	client.UserSubscribe.Create().SetUserID(account.ID).SetOrderID(2).SetSubscribeID(expired.ID).
		SetStatus(1).SetExpireTime(recentlyExpired).SetFinishedAt(recentlyExpired).SaveX(ctx)

	baseURL := startDeviceWebSocketServer(t, service)
	path := fmt.Sprintf("/socket/%d/", account.ID)
	connect := func(number int) (*websocket.Conn, string) {
		t.Helper()
		identifier := fmt.Sprintf("device-%d", number)
		token, err := usersession.Issue(ctx, cache, service.Config.JwtAuth.AccessSecret,
			service.Config.JwtAuth.AccessExpire, account.ID, fmt.Sprintf("session-%d", number), "oauth", "", "")
		require.NoError(t, err)
		dialer := websocket.Dialer{HandshakeTimeout: timeout}
		conn, response, err := dialer.Dial(baseURL+path+identifier, http.Header{"Authorization": {token}})
		if err != nil && response != nil {
			_ = response.Body.Close()
		}
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })
		require.NoError(t, conn.SetWriteDeadline(time.Now().Add(timeout)))
		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("heartbeat")))
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(timeout)))
		_, reply, err := conn.ReadMessage()
		require.NoError(t, err)
		require.Equal(t, "ping", string(reply))
		return conn, token
	}

	first, firstToken := connect(1)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		row, err := client.UserDevice.Get(ctx, oldest.ID)
		require.NoError(c, err)
		require.True(c, row.Online)
	}, timeout, 10*time.Millisecond)
	for number := 2; number <= 4; number++ {
		connect(number)
	}
	_, err := middleware.AuthenticateRequest(ctx, service, firstToken, path)
	require.NoError(t, err, "the active plan allows a fourth device")

	_, latestToken := connect(5)
	require.NoError(t, first.SetReadDeadline(time.Now().Add(timeout)))
	_, payload, err := first.ReadMessage()
	require.NoError(t, err)
	require.JSONEq(t, (&svc.DeviceMessage{Method: svc.DeviceKickedMax}).Json(), string(payload))
	_, err = middleware.AuthenticateRequest(ctx, service, firstToken, path)
	var code *xerr.CodeError
	require.ErrorAs(t, err, &code)
	require.Equal(t, xerr.InvalidAccess, code.GetErrCode())
	_, err = middleware.AuthenticateRequest(ctx, service, latestToken, path)
	require.NoError(t, err)

	latest := client.UserDevice.Query().Where(userdevice.Identifier("device-5")).OnlyX(ctx)
	require.Equal(t, account.ID, latest.UserID)
	require.True(t, latest.Enabled)
	require.True(t, latest.Online)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		record, err := client.UserDeviceOnlineRecord.Query().Where(userdeviceonlinerecord.Identifier(oldest.Identifier)).Only(ctx)
		require.NoError(c, err)
		require.Equal(c, account.ID, record.UserID)
		row, err := client.UserDevice.Get(ctx, oldest.ID)
		require.NoError(c, err)
		require.False(c, row.Online)
		require.True(c, row.Enabled)
	}, timeout, 10*time.Millisecond)
}

func startDeviceWebSocketServer(t *testing.T, service *svc.ServiceContext) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	service.DeviceManager = svc.NewDeviceManager(service)
	engine := server.New(server.WithListener(listener), server.WithTransport(standard.NewTransporter), server.WithDisablePrintRoute(true))
	engine.GET("/socket/:userid/:identifier", func(ctx context.Context, raw *app.RequestContext) {
		request := hertzx.NewContext(ctx, raw)
		ctx, err := middleware.AuthenticateRequest(ctx, service, request.GetHeader("Authorization"), request.Request.URL.Path)
		if err != nil {
			raw.String(http.StatusUnauthorized, "%v", err)
			return
		}
		if err := NewDeviceWsConnectLogic(ctx, service).DeviceWsConnect(request); err != nil {
			raw.String(http.StatusBadRequest, "%v", err)
		}
	})
	stopped := make(chan error, 1)
	go func() { stopped <- engine.Run() }()
	t.Cleanup(func() {
		service.DeviceManager.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := engine.Shutdown(ctx); err != nil {
			t.Errorf("shutdown Hertz server: %v", err)
			_ = listener.Close()
		}
		select {
		case err := <-stopped:
			if err != nil {
				t.Errorf("run Hertz server: %v", err)
			}
		case <-ctx.Done():
			t.Error("Hertz server did not stop")
		}
	})
	return "ws://" + listener.Addr().String()
}
