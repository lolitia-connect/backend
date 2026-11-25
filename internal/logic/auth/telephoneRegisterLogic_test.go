package auth

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func TestTelephoneRegisterCreatesAccountAndSession(t *testing.T) {
	database, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	mini := miniredis.RunT(t)
	cache := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, cache), Redis: cache}
	service.Config.Mobile.Enable = true
	service.Config.JwtAuth.AccessSecret = "telephone-registration-test-secret"
	service.Config.JwtAuth.AccessExpire = 3600
	ctx := context.Background()
	key := fmt.Sprintf("%s:%s:%s", config.AuthCodeTelephoneCacheKey, constant.ParseVerifyType(uint8(constant.Register)), "+12025550123")
	if err := cache.Set(ctx, key, `{"code":"123456"}`, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	request := &types.TelephoneRegisterRequest{TelephoneAreaCode: "1", Telephone: "2025550123", Password: "test-only-password", Code: "123456", IP: "192.0.2.1", UserAgent: "test-agent"}
	response, err := NewTelephoneRegisterLogic(ctx, service).TelephoneRegister(request)
	if err != nil || response == nil || response.Token == "" {
		t.Fatalf("registration did not return a session: response=%+v, err=%v", response, err)
	}
	if client.User.Query().CountX(ctx) != 1 || client.UserAuthMethod.Query().OnlyX(ctx).AuthIdentifier != "+12025550123" || cache.Exists(ctx, key).Val() != 0 {
		t.Fatal("telephone account was not persisted or verification code was not consumed")
	}
}

func TestTelephoneCodeLoginAndRecovery(t *testing.T) {
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
	service.Config.Mobile.Enable = true
	service.Config.JwtAuth.AccessSecret = "telephone-session-fixture"
	service.Config.JwtAuth.AccessExpire = 3600
	account := client.User.Create().SetPassword("initial-fixture-hash").SaveX(ctx)
	client.UserAuthMethod.Create().SetUserID(account.ID).SetAuthType("mobile").SetAuthIdentifier("+12025550123").SetVerified(true).SaveX(ctx)
	key := fmt.Sprintf("%s:%s:%s", config.AuthCodeTelephoneCacheKey, constant.Security, "+12025550123")
	setCode := func(code string) {
		t.Helper()
		if err := cache.Set(ctx, key, fmt.Sprintf(`{"code":%q}`, code), time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
	}
	assertSession := func(response *types.LoginResponse, err error) {
		t.Helper()
		if err != nil || response == nil || response.Token == "" {
			t.Fatalf("expected phone session, got %+v, %v", response, err)
		}
		authenticated, err := middleware.AuthenticateRequest(ctx, service, response.Token, "/v1/public/user/info")
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := authenticated.Value(constant.CtxKeyUser).(*user.User); !ok || got.Id != account.ID {
			t.Fatal("phone session authenticated as the wrong account")
		}
	}
	login := NewTelephoneLoginLogic(ctx, service)
	request := &types.TelephoneLoginRequest{TelephoneAreaCode: "1", Telephone: "2025550123", TelephoneCode: "123456", LoginType: "mobile"}
	httpRequest := httptest.NewRequest(http.MethodPost, "/", nil)
	setCode(request.TelephoneCode)
	first, err := login.TelephoneLogin(request, httpRequest, "192.0.2.1")
	assertSession(first, err)
	if replay, err := login.TelephoneLogin(request, httpRequest, "192.0.2.1"); err == nil || replay != nil {
		t.Fatal("used SMS code produced another session")
	}

	setCode("654321")
	reset := NewTelephoneResetPasswordLogic(ctx, service)
	resetRequest := &types.TelephoneResetPasswordRequest{TelephoneAreaCode: "1", Telephone: "2025550123", Code: "123456", Password: "new-fixture-password", LoginType: "mobile"}
	if rejected, err := reset.TelephoneResetPassword(resetRequest); err == nil || rejected != nil || client.User.GetX(ctx, account.ID).Password != account.Password {
		t.Fatal("wrong SMS recovery code changed the account")
	}
	resetRequest.Code = "654321"
	assertSession(reset.TelephoneResetPassword(resetRequest))
	if _, err := middleware.AuthenticateRequest(ctx, service, first.Token, "/v1/public/user/info"); err == nil {
		t.Fatal("password recovery left the prior phone session active")
	}
	request.TelephoneCode, request.Password = "", resetRequest.Password
	assertSession(login.TelephoneLogin(request, httpRequest, "192.0.2.1"))
}

func TestTelephoneRegisterHonorsDisabledRegistration(t *testing.T) {
	service := &svc.ServiceContext{}
	service.Config.Register.StopRegister = true
	if response, err := NewTelephoneRegisterLogic(context.Background(), service).TelephoneRegister(&types.TelephoneRegisterRequest{}); err == nil || response != nil {
		t.Fatalf("disabled registration returned success: %+v, %v", response, err)
	}
}
