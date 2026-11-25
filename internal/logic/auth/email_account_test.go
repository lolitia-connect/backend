package auth

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/ent/systemlog"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func TestEmailAccountRegistrationLoginAndPasswordReset(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, cache), Redis: cache}
	service.Config.JwtAuth.AccessSecret = "email-account-fixture-secret"
	service.Config.JwtAuth.AccessExpire = 3600
	service.Config.Email.EnableVerify = true
	service.Config.Invite.ForcedInvite = true
	service.Config.Register.EnableIpRegisterLimit = true
	service.Config.Register.IpRegisterLimit = 2
	service.Config.Register.IpRegisterLimitDuration = 60
	inviter := client.User.Create().SetPassword("fixture").SetReferCode("account-test-invite").SaveX(ctx)
	const email = "account@example.test"
	setCode := func(kind constant.VerifyType, code string) {
		t.Helper()
		key := fmt.Sprintf("%s:%s:%s", config.AuthCodeCacheKey, kind, email)
		if err := cache.Set(ctx, key, fmt.Sprintf(`{"code":%q}`, code), time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
	}
	const original = "registration-fixture-password"
	req := &types.UserRegisterRequest{Email: email, Password: original, IP: "192.0.2.44", UserAgent: "Example browser", LoginType: "email"}
	register := NewUserRegisterLogic(ctx, service)
	if got, err := register.UserRegister(req); err == nil || got != nil {
		t.Fatal("registration with required invitation missing succeeded")
	}
	req.Invite, req.Code = inviter.ReferCode, "123456"
	setCode(constant.Register, req.Code)
	created, err := register.UserRegister(req)
	if err != nil || created == nil || created.Token == "" {
		t.Fatalf("register: %v", err)
	}
	credential := client.UserAuthMethod.Query().OnlyX(ctx)
	account := client.User.GetX(ctx, credential.UserID)
	if credential.AuthIdentifier != email || !credential.Verified || account.RefererID != inviter.ID || account.ReferCode == "" {
		t.Fatal("registration did not persist verified email and invitation")
	}
	checkSession := func(token string) {
		t.Helper()
		current, err := middleware.AuthenticateRequest(ctx, service, token, "/v1/public/user/info")
		if err != nil {
			t.Fatalf("authenticate session: %v", err)
		}
		got, ok := current.Value(constant.CtxKeyUser).(*user.User)
		if !ok || got.Id != account.ID {
			t.Fatal("session authenticated as another account")
		}
	}
	checkSession(created.Token)
	login := NewUserLoginLogic(ctx, service)
	attempt := &types.UserLoginRequest{Email: email, Password: original, IP: req.IP, UserAgent: req.UserAgent, LoginType: "email"}
	loggedIn, err := login.UserLogin(attempt)
	if err != nil || loggedIn == nil || loggedIn.Token == "" {
		t.Fatalf("login: %v", err)
	}
	checkSession(loggedIn.Token)

	const next = "reset-fixture-password"
	reset := NewResetPasswordLogic(ctx, service)
	setCode(constant.Security, "876543")
	resetReq := &types.ResetPasswordRequest{Email: email, Password: next, Code: "incorrect", IP: req.IP, UserAgent: req.UserAgent, LoginType: "email"}
	if got, err := reset.ResetPassword(resetReq); err == nil || got != nil {
		t.Fatal("wrong reset code was accepted")
	}
	if got := client.User.GetX(ctx, account.ID); got.Password != account.Password {
		t.Fatal("failed reset changed password")
	}
	resetReq.Code = "876543"
	updated, err := reset.ResetPassword(resetReq)
	if err != nil || updated == nil || updated.Token == "" {
		t.Fatalf("reset: %v", err)
	}
	checkSession(updated.Token)
	for _, token := range []string{created.Token, loggedIn.Token} {
		if _, err := middleware.AuthenticateRequest(ctx, service, token, "/v1/public/user/info"); err == nil {
			t.Fatal("password reset left a previous session active")
		}
	}
	if got, err := login.UserLogin(attempt); err == nil || got != nil {
		t.Fatal("old password remained usable")
	}
	attempt.Password = next
	if got, err := login.UserLogin(attempt); err != nil || got == nil || got.Token == "" {
		t.Fatalf("login after reset: %v", err)
	}
	entries := client.SystemLog.Query().Where(systemlog.TypeEQ(log.TypeLogin.Uint8()), systemlog.ObjectIDEQ(account.ID)).AllX(ctx)
	var passed, failed int
	for _, entry := range entries {
		var event log.Login
		if err := event.Unmarshal([]byte(entry.Content)); err != nil || event.LoginIP != req.IP || event.Method != "email" {
			t.Fatalf("login audit = %+v, %v", event, err)
		}
		if event.Success {
			passed++
		} else {
			failed++
		}
	}
	if passed != 4 || failed != 1 {
		t.Fatalf("login audit success/failure counts = %d/%d, want 4/1", passed, failed)
	}
}
