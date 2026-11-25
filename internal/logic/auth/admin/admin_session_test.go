package admin

import (
	"database/sql"
	"errors"
	"fmt"
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
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func TestAdminLoginAndResetRequireAdminAccount(t *testing.T) {
	ctx := t.Context()
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
	service.Config.JwtAuth.AccessSecret = "admin-test-token-secret"
	service.Config.JwtAuth.AccessExpire = 600

	const original = "original-fixture-password"
	const replacement = "replacement-fixture-password"
	const adminEmail = "operator@example.test"
	const customerEmail = "customer@example.test"
	hash := tool.EncodePassWord(original)
	createAccount := func(email string, administrator bool) *ent.User {
		t.Helper()
		account := client.User.Create().SetPassword(hash).SetAlgo(tool.PasswordAlgoArgon2id).SetIsAdmin(administrator).SaveX(ctx)
		client.UserAuthMethod.Create().SetUserID(account.ID).SetAuthType("email").SetAuthIdentifier(email).SetVerified(true).SaveX(ctx)
		key := fmt.Sprintf("%s:%s:%s", config.AuthCodeCacheKey, constant.Security, email)
		if err := cache.Set(ctx, key, `{"code":"123456"}`, time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
		return account
	}
	admin := createAccount(adminEmail, true)
	customer := createAccount(customerEmail, false)
	login := NewAdminLoginLogic(ctx, service)
	reset := NewAdminResetPasswordLogic(ctx, service)
	assertDenied := func(resp *types.LoginResponse, err error) {
		t.Helper()
		var code *xerr.CodeError
		if resp != nil || !errors.As(err, &code) || code.GetErrCode() != xerr.PermissionDenied {
			t.Fatalf("expected administrator restriction, got response=%+v error=%v", resp, err)
		}
	}
	assertDenied(login.AdminLogin(&types.UserLoginRequest{Email: customerEmail, Password: original}))
	assertDenied(reset.AdminResetPassword(&types.ResetPasswordRequest{Email: customerEmail, Code: "123456", Password: replacement}))
	if current := client.User.GetX(ctx, customer.ID); current.Password != hash || current.IsAdmin {
		t.Fatal("admin recovery changed a customer account")
	}

	const path = "/v1/admin/user"
	assertSession := func(resp *types.LoginResponse, err error) string {
		t.Helper()
		if err != nil || resp == nil || resp.Token == "" {
			t.Fatalf("admin session was not issued: %v", err)
		}
		authenticated, err := middleware.AuthenticateRequest(ctx, service, resp.Token, path)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := authenticated.Value(constant.CtxKeyUser).(*user.User); !ok || got.Id != admin.ID {
			t.Fatal("session authenticated as a different administrator")
		}
		return resp.Token
	}
	old := assertSession(login.AdminLogin(&types.UserLoginRequest{Email: adminEmail, Password: original, LoginType: "email"}))
	assertSession(reset.AdminResetPassword(&types.ResetPasswordRequest{Email: adminEmail, Code: "123456", Password: replacement, LoginType: "email"}))
	if _, err := middleware.AuthenticateRequest(ctx, service, old, path); err == nil {
		t.Fatal("previous admin session survived password reset")
	}
	if resp, err := login.AdminLogin(&types.UserLoginRequest{Email: adminEmail, Password: original}); resp != nil || err == nil {
		t.Fatal("old admin password still works")
	}
	assertSession(login.AdminLogin(&types.UserLoginRequest{Email: adminEmail, Password: replacement, LoginType: "email"}))
}
