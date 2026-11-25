package auth

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func TestTrialRegistrationPersistsEntitlementBeforeIssuingSession(t *testing.T) {
	ctx := t.Context()
	database, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, cache), Redis: cache}
	service.Config.JwtAuth.AccessSecret = "trial-registration-fixture"
	service.Config.JwtAuth.AccessExpire = 300
	service.Config.Register.EnableTrial = true
	service.Config.Register.TrialTime = 2
	service.Config.Register.TrialTimeUnit = "Hour"
	plan := client.Subscribe.Create().SetName("Trial").SetTraffic(5000).SaveX(ctx)
	service.Config.Register.TrialSubscribe = plan.ID + 1

	router := hertzx.New()
	router.POST("/register", UserRegisterHandler(service))
	register := func(body string) (uint32, string) {
		t.Helper()
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		var result struct {
			Code uint32              `json:"code"`
			Data types.LoginResponse `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatalf("register HTTP %d: %s (%v)", recorder.Code, recorder.Body, err)
		}
		return result.Code, result.Data.Token
	}
	const body = `{"email":"trial@example.test","password":"trial-fixture-password"}`
	if code, token := register(`{"email":"trial@example.test"}`); code != xerr.InvalidParams || token != "" {
		t.Fatalf("missing password returned code %d and token=%v", code, token != "")
	}
	if code, token := register(body); code == http.StatusOK || token != "" {
		t.Fatalf("missing trial plan returned code %d and token=%v", code, token != "")
	}
	if client.User.Query().CountX(ctx) != 0 || client.UserAuthMethod.Query().CountX(ctx) != 0 || client.UserSubscribe.Query().CountX(ctx) != 0 {
		t.Fatal("registration with a missing trial plan left a partial account")
	}

	service.Config.Register.TrialSubscribe = plan.ID
	code, token := register(body)
	if code != http.StatusOK || token == "" {
		t.Fatalf("trial registration returned code %d and token=%v", code, token != "")
	}
	authenticated, err := middleware.AuthenticateRequest(ctx, service, token, "/v1/public/user/info")
	if err != nil {
		t.Fatal(err)
	}
	account, ok := authenticated.Value(constant.CtxKeyUser).(*user.User)
	entitlement := client.UserSubscribe.Query().OnlyX(ctx)
	credential := client.UserAuthMethod.Query().OnlyX(ctx)
	if !ok || credential.UserID != account.Id || credential.AuthIdentifier != "trial@example.test" || entitlement.UserID != account.Id ||
		entitlement.SubscribeID != plan.ID || entitlement.Traffic != 5000 || entitlement.Status != 1 || entitlement.OrderID != 0 ||
		entitlement.Token == "" || !entitlement.ExpireTime.After(time.Now().Add(time.Hour)) {
		t.Fatalf("trial was not provisioned for the authenticated account: %v", entitlement.ID)
	}
	if code, token := register(body); code != xerr.UserExist || token != "" || client.UserSubscribe.Query().CountX(ctx) != 1 {
		t.Fatal("duplicate registration issued another trial or session")
	}
}
