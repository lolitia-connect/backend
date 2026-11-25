package handler

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
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/usersession"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

type routeFixture struct {
	t       *testing.T
	client  *ent.Client
	service *svc.ServiceContext
	router  *hertzx.Engine
}

func newRouteFixture(t *testing.T) *routeFixture {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, cache), Redis: cache}
	service.Config.JwtAuth.AccessSecret = "route-fixture-secret"
	router := hertzx.New()
	RegisterHandlers(router, service)
	return &routeFixture{t: t, client: client, service: service, router: router}
}

func (f *routeFixture) startQueue() *asynq.Inspector {
	f.t.Helper()
	options := asynq.RedisClientOpt{Addr: f.service.Redis.Options().Addr}
	client := asynq.NewClient(options)
	inspector := asynq.NewInspector(options)
	f.service.Queue = client
	f.t.Cleanup(func() { _ = inspector.Close(); _ = client.Close() })
	return inspector
}

func (f *routeFixture) account(admin bool) (*ent.User, string) {
	f.t.Helper()
	ctx := f.t.Context()
	account := f.client.User.Create().SetPassword("fixture").SetEnable(true).SetIsAdmin(admin).SaveX(ctx)
	token, err := usersession.Issue(ctx, f.service.Redis, f.service.Config.JwtAuth.AccessSecret, 300, account.ID, "", "email", "", "")
	if err != nil {
		f.t.Fatal(err)
	}
	return account, token
}

func (f *routeFixture) request(method, path, token, body string, want uint32) json.RawMessage {
	f.t.Helper()
	req := httptest.NewRequestWithContext(f.t.Context(), method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	return f.serve(req, http.StatusOK, want)
}

func (f *routeFixture) serve(req *http.Request, wantHTTP int, want uint32) json.RawMessage {
	f.t.Helper()
	recorder := httptest.NewRecorder()
	f.router.ServeHTTP(recorder, req)
	var reply struct {
		Code uint32          `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &reply); err != nil || recorder.Code != wantHTTP || reply.Code != want {
		f.t.Fatalf("%s %s: HTTP %d, %s (want HTTP %d, code %d, decode error %v)", req.Method, req.URL.Path, recorder.Code, recorder.Body.String(), wantHTTP, want, err)
	}
	return reply.Data
}
