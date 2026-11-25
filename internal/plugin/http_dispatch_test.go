package plugin_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	pluginv1 "github.com/perfect-panel/server/api/plugin/v1"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/handler"
	"github.com/perfect-panel/server/internal/plugin"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/usersession"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newPluginHTTPService(t *testing.T, admin bool) (*svc.ServiceContext, int64, string) {
	t.Helper()
	ctx := t.Context()
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, cache), Redis: cache}
	service.Config.JwtAuth.AccessSecret = "dispatch-fixture"
	member := client.User.Create().SetEnable(true).SetIsAdmin(admin).SetPassword("fixture").SaveX(ctx)
	token, err := usersession.Issue(ctx, cache, service.Config.JwtAuth.AccessSecret, 300, member.ID, "", "email", "", "")
	require.NoError(t, err)
	return service, member.ID, token
}

func TestPluginManagementClientsShareTheRunningCatalog(t *testing.T) {
	service, _, operator := newPluginHTTPService(t, true)
	mgr, _ := plugin.StartDispatchTestGuest(t, nil)
	service.PluginMgr = mgr
	router := hertzx.New()
	handler.RegisterNativeHandlers(router.Hertz(), service)
	handler.RegisterPluginHandlers(router, service, mgr)
	const name = "lifecycle-guest"
	const legacy = "/v1/admin/plugin/"
	const installed = "/v1/admin/plugins/" + name
	const catalog = "/v1/plugin/" + name + "/catalog"
	request := func(method, path, form string, status int) []byte {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(form))
		if strings.HasPrefix(path, "/v1/admin/") {
			req.Header.Set("Authorization", operator)
		}
		if form != "" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, status, rec.Code, "%s %s: %s", method, path, rec.Body.String())
		return rec.Body.Bytes()
	}
	readState := func(want plugin.PluginStatus) {
		t.Helper()
		var record struct {
			Code int               `json:"code"`
			Data plugin.PluginInfo `json:"data"`
		}
		require.NoError(t, json.Unmarshal(request(http.MethodGet, installed, "", http.StatusOK), &record))
		require.Equal(t, http.StatusOK, record.Code)
		require.Equal(t, name, record.Data.Name)
		require.Equal(t, "catalog", record.Data.Version)
		require.Equal(t, want, record.Data.Status)
		var loaded []plugin.PluginInfo
		require.NoError(t, json.Unmarshal(request(http.MethodGet, legacy+"list", "", http.StatusOK), &loaded))
		if want == plugin.StatusRunning {
			require.Len(t, loaded, 1)
			require.Equal(t, record.Data.Name, loaded[0].Name)
			require.Equal(t, record.Data.Status, loaded[0].Status)
			require.Equal(t, "catalog", string(request(http.MethodGet, catalog, "", http.StatusOK)))
		} else {
			require.Empty(t, loaded)
			request(http.MethodGet, legacy+"detail?name="+name, "", http.StatusNotFound)
			request(http.MethodGet, catalog, "", http.StatusNotFound)
		}
	}
	action := func(verb string, want plugin.PluginStatus) {
		t.Helper()
		var reply struct {
			Code int `json:"code"`
			Data struct {
				Name, Action string
				Status       plugin.PluginStatus
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(request(http.MethodPost, installed+"/"+verb, "", http.StatusOK), &reply))
		require.Equal(t, http.StatusOK, reply.Code)
		require.Equal(t, name, reply.Data.Name)
		require.Equal(t, verb, reply.Data.Action)
		require.Equal(t, want, reply.Data.Status)
	}
	readState(plugin.StatusRunning)
	request(http.MethodPost, legacy+"disable", "name="+name, http.StatusOK)
	readState(plugin.StatusUnloaded)
	action("enable", plugin.StatusRunning)
	request(http.MethodPost, legacy+"reload", "name="+name, http.StatusOK)
	readState(plugin.StatusRunning)
	action("disable", plugin.StatusUnloaded)
	readState(plugin.StatusUnloaded)
	request(http.MethodPost, legacy+"enable", "name="+name, http.StatusOK)
	action("restart", plugin.StatusRunning)
	readState(plugin.StatusRunning)
}

func TestPluginHTTPDispatch(t *testing.T) {
	service, memberID, token := newPluginHTTPService(t, false)
	const path = "/v1/plugin/lifecycle-guest/report"
	const payload = `{"period":"weekly"}`
	for _, tc := range []struct {
		name       string
		middleware *pluginv1.MiddlewareResponse
	}{
		{"submit report", &pluginv1.MiddlewareResponse{Action: "modify", Headers: map[string]string{"X-Report-Format": "compact"}}},
		{"maintenance", &pluginv1.MiddlewareResponse{Action: "abort", Status: http.StatusServiceUnavailable, Headers: map[string]string{"Retry-After": "120"}, Body: []byte("reports paused")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr, lastRequest := plugin.StartDispatchTestGuest(t, tc.middleware)
			router := hertzx.New()
			handler.RegisterPluginHandlers(router, service, mgr)
			submit := func() *httptest.ResponseRecorder {
				t.Helper()
				req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path+"?region=west&region=east", strings.NewReader(payload))
				req.Header.Set("Authorization", token)
				req.Header.Set("Content-Type", "application/json")
				rr := httptest.NewRecorder()
				router.ServeHTTP(rr, req)
				return rr
			}
			resp := submit()
			if tc.middleware.Action == "abort" {
				require.Equal(t, http.StatusServiceUnavailable, resp.Code)
				require.Equal(t, "120", resp.Header().Get("Retry-After"))
				require.Equal(t, "reports paused", resp.Body.String())
				require.Nil(t, lastRequest(), "maintenance response must finish before the report handler runs")
			} else {
				require.Equal(t, http.StatusAccepted, resp.Code, resp.Body.String())
				require.Equal(t, "catalog", resp.Header().Get("X-Report-Version"))
				require.Equal(t, "report accepted", resp.Body.String())
				received := lastRequest()
				require.NotNil(t, received)
				require.Equal(t, http.MethodPost, received.Method)
				require.Equal(t, path, received.Path)
				require.Equal(t, []string{"west", "east"}, received.Query["region"].GetValues())
				require.Equal(t, []string{"compact"}, received.Headers["X-Report-Format"].GetValues())
				require.Equal(t, payload, string(received.Body))
				require.Equal(t, memberID, received.GetContext().GetUserId())
				require.False(t, received.GetContext().GetIsAdmin())
			}
			require.NoError(t, mgr.DisablePlugin("lifecycle-guest"))
			require.Equal(t, http.StatusNotFound, submit().Code)
		})
	}
}
