package oauth

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func newOAuthTestService(t *testing.T) (*svc.ServiceContext, *ent.Client, *redis.Client, context.Context) {
	t.Helper()
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
	service.Config.JwtAuth.AccessSecret = "oauth-registration-test-secret"
	service.Config.JwtAuth.AccessExpire = 3600
	return service, client, cache, context.Background()
}

func authMethods(t *testing.T, client *ent.Client, ctx context.Context) []*ent.UserAuthMethod {
	t.Helper()
	return client.UserAuthMethod.Query().AllX(ctx)
}

// A provider that returns no email (Telegram) must not fabricate a placeholder
// address; registration is deferred until the user supplies a real email.
func TestOAuthRegisterRequiresRealEmailWhenProviderHasNone(t *testing.T) {
	service, client, _, ctx := newOAuthTestService(t)

	loginLogic := NewOAuthLoginGetTokenLogic(ctx, service)
	_, err := loginLogic.findOrRegisterUser(OAuthTelegram, "900001", "", "avatar", "", true, "req-1", "192.0.2.1", "agent")
	pending, ok := err.(*oauthRegistrationPending)
	if !ok {
		t.Fatalf("expected pending registration, got err=%v", err)
	}
	if !pending.emailRequired || pending.inviteRequired {
		t.Fatalf("expected email required only, got %+v", pending)
	}
	if client.User.Query().CountX(ctx) != 0 || len(authMethods(t, client, ctx)) != 0 {
		t.Fatal("no account should be created while an email is still required")
	}

	regLogic := NewOAuthRegisterLogic(ctx, service)
	resp, err := regLogic.OAuthRegister(&types.OAuthRegisterRequest{OAuthToken: pending.ticket, Email: "real-user@example.com"}, "192.0.2.1", "agent")
	if err != nil || resp == nil || resp.Token == "" {
		t.Fatalf("completing registration with a real email failed: resp=%+v, err=%v", resp, err)
	}

	methods := authMethods(t, client, ctx)
	if client.User.Query().CountX(ctx) != 1 || len(methods) != 2 {
		t.Fatalf("expected one user with provider and email identities, got %d users and %d methods", client.User.Query().CountX(ctx), len(methods))
	}
	var emailMethod *ent.UserAuthMethod
	for _, m := range methods {
		if strings.Contains(m.AuthIdentifier, "@ppanel.com") {
			t.Fatalf("placeholder email was persisted: %s", m.AuthIdentifier)
		}
		if m.AuthType == AuthEmail {
			emailMethod = m
		}
	}
	if emailMethod == nil || emailMethod.AuthIdentifier != "real-user@example.com" {
		t.Fatalf("user-supplied email was not stored: %+v", emailMethod)
	}
	// Email verification is disabled here, so the address must not be trusted.
	if emailMethod.Verified {
		t.Fatal("an unverified email must not be stored as verified")
	}
}

// When email verification is enabled, the collected email is only trusted once
// its verification code checks out, matching ordinary email registration.
func TestOAuthRegisterVerifiesCollectedEmailWithCode(t *testing.T) {
	service, client, cache, ctx := newOAuthTestService(t)
	service.Config.Email.EnableVerify = true

	loginLogic := NewOAuthLoginGetTokenLogic(ctx, service)
	_, err := loginLogic.findOrRegisterUser(OAuthTelegram, "900002", "", "", "", true, "req-2", "192.0.2.1", "agent")
	pending, ok := err.(*oauthRegistrationPending)
	if !ok || !pending.emailRequired || !pending.emailVerify {
		t.Fatalf("expected email verification to be required, got err=%v pending=%+v", err, pending)
	}

	regLogic := NewOAuthRegisterLogic(ctx, service)
	// A wrong or missing code is rejected and writes nothing.
	if _, err := regLogic.OAuthRegister(&types.OAuthRegisterRequest{OAuthToken: pending.ticket, Email: "verify-me@example.com", Code: "000000"}, "192.0.2.1", "agent"); err == nil {
		t.Fatal("registration should fail without a valid verification code")
	}
	if client.User.Query().CountX(ctx) != 0 {
		t.Fatal("no account should be created when the code is wrong")
	}

	cacheKey := fmt.Sprintf("%s:%s:%s", config.AuthCodeCacheKey, constant.Register, "verify-me@example.com")
	if err := cache.Set(ctx, cacheKey, `{"code":"123456"}`, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	resp, err := regLogic.OAuthRegister(&types.OAuthRegisterRequest{OAuthToken: pending.ticket, Email: "verify-me@example.com", Code: "123456"}, "192.0.2.1", "agent")
	if err != nil || resp == nil || resp.Token == "" {
		t.Fatalf("registration with a valid code failed: resp=%+v, err=%v", resp, err)
	}
	for _, m := range authMethods(t, client, ctx) {
		if m.AuthType == AuthEmail && !m.Verified {
			t.Fatal("a code-verified email must be stored as verified")
		}
	}
}

// A provider that supplies a real email registers directly and trusts it.
func TestOAuthRegisterTrustsProviderSuppliedEmail(t *testing.T) {
	service, client, _, ctx := newOAuthTestService(t)

	loginLogic := NewOAuthLoginGetTokenLogic(ctx, service)
	userInfo, err := loginLogic.findOrRegisterUser(OAuthGoogle, "google-123", "person@gmail.com", "", "", true, "req-3", "192.0.2.1", "agent")
	if err != nil || userInfo == nil || userInfo.Id == 0 {
		t.Fatalf("provider-supplied email should register immediately: user=%+v, err=%v", userInfo, err)
	}
	var emailMethod *ent.UserAuthMethod
	for _, m := range authMethods(t, client, ctx) {
		if m.AuthType == AuthEmail {
			emailMethod = m
		}
	}
	if emailMethod == nil || emailMethod.AuthIdentifier != "person@gmail.com" || !emailMethod.Verified {
		t.Fatalf("provider email should be stored as verified: %+v", emailMethod)
	}
}
