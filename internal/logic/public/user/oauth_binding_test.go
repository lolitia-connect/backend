package user

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/config"
	usermodel "github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/oauthstate"
	"github.com/perfect-panel/server/pkg/xerr"
)

func TestTelegramCallbackPersistsOneVerifiedBinding(t *testing.T) {
	service, client, _ := newAccountFixture(t)
	ctx := t.Context()
	owner := client.User.Create().SetPassword("fixture").SaveX(ctx)
	other := client.User.Create().SetPassword("fixture").SaveX(ctx)
	const botToken = "123456789:fixture-token"
	client.AuthMethod.Create().SetMethod("telegram").SetConfig(`{"bot_token":"` + botToken + `"}`).SetEnabled(true).SaveX(ctx)
	callback := func(chatID, authDate int64, sign bool) string {
		t.Helper()
		key := sha256.Sum256([]byte(botToken))
		mac := hmac.New(sha256.New, key[:])
		_, _ = fmt.Fprintf(mac, "auth_date=%d\nid=%d", authDate, chatID)
		signature := mac.Sum(nil)
		if !sign {
			signature[0] ^= 1
		}
		payload := fmt.Sprintf(`{"auth_date":%d,"id":%d,"hash":"%x"}`, authDate, chatID, signature)
		return base64.RawURLEncoding.EncodeToString([]byte(payload))
	}
	bind := func(userID int64, payload string) error {
		account := context.WithValue(ctx, constant.CtxKeyUser, &usermodel.User{Id: userID})
		return NewBindOAuthCallbackLogic(account, service).BindOAuthCallback(&types.BindOAuthCallbackRequest{
			Method:   "telegram",
			Callback: map[string]any{"tgAuthResult": payload},
		})
	}
	now := time.Now().Unix()
	for _, payload := range []string{callback(42, now, false), callback(42, now-301, true)} {
		if err := bind(owner.ID, payload); err == nil || client.UserAuthMethod.Query().CountX(ctx) != 0 {
			t.Fatal("invalid or expired callback created a binding")
		}
	}
	payload := callback(42, now, true)
	for range 2 {
		if err := bind(owner.ID, payload); err != nil {
			t.Fatalf("bind or retry: %v", err)
		}
	}
	for _, test := range []struct {
		userID  int64
		payload string
	}{{other.ID, payload}, {owner.ID, callback(43, now, true)}} {
		var code *xerr.CodeError
		if err := bind(test.userID, test.payload); !errors.As(err, &code) || code.GetErrCode() != xerr.UserExist {
			t.Fatalf("conflicting binding returned %v", err)
		}
	}
	claimKey := config.TelegramCallbackKey + ":" + oauthstate.PayloadFingerprint(payload)
	if err := service.Redis.Set(ctx, claimKey, now-61, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if err := bind(owner.ID, payload); err == nil {
		t.Fatal("callback was accepted after its retry grace period")
	}
	binding := client.UserAuthMethod.Query().OnlyX(ctx)
	if binding.UserID != owner.ID || binding.AuthType != "telegram" || binding.AuthIdentifier != "42" || !binding.Verified {
		t.Fatalf("persisted Telegram binding = %+v", binding)
	}
}

func TestOAuthBindingCreatesAccountScopedProviderState(t *testing.T) {
	service, client, cache := newAccountFixture(t)
	ctx := t.Context()
	const redirect = "https://panel.example.test/account/link"
	service.Config.Site.Host = "https://panel.example.test"
	owner := client.User.Create().SetPassword("fixture").SaveX(ctx)
	other := client.User.Create().SetPassword("fixture").SaveX(ctx)
	logic := NewBindOAuthLogic(context.WithValue(ctx, constant.CtxKeyUser, &usermodel.User{Id: owner.ID}), service)
	for _, tc := range []struct {
		method, config, host, stateParam, redirectParam, target string
	}{
		{"google", `{"client_id":"panel"}`, "accounts.google.com", "state", "redirect_uri", redirect},
		{"github", `{"client_id":"panel"}`, "github.com", "state", "redirect_uri", redirect},
		{"facebook", `{"client_id":"panel"}`, "www.facebook.com", "state", "redirect_uri", redirect},
		{"apple", `{"client_id":"panel","redirect_url":"https://api.example.test"}`, "appleid.apple.com", "state", "redirect_uri", "https://api.example.test/v1/auth/oauth/callback/apple"},
		{"telegram", `{"bot_token":"123456789:fixture-token"}`, "oauth.telegram.org", "embed", "return_to", redirect},
	} {
		t.Run(tc.method, func(t *testing.T) {
			client.AuthMethod.Create().SetMethod(tc.method).SetConfig(tc.config).SetEnabled(true).SaveX(ctx)
			response, err := logic.BindOAuth(&types.BindOAuthRequest{Method: tc.method, Redirect: redirect})
			if err != nil || response == nil {
				t.Fatalf("start linking %s: %v", tc.method, err)
			}
			provider, err := url.Parse(response.Redirect)
			if err != nil || provider.Scheme != "https" || provider.Host != tc.host || provider.Query().Get(tc.redirectParam) != tc.target {
				t.Fatalf("provider link = %q, %v", response.Redirect, err)
			}
			state := provider.Query().Get(tc.stateParam)
			if _, err := oauthstate.Consume(ctx, service.Redis, tc.method, "bind", other.ID, state); !oauthstate.IsRejected(err) {
				t.Fatalf("other account consumed state: %v", err)
			}
			if got, err := oauthstate.Consume(ctx, service.Redis, tc.method, "bind", owner.ID, state); err != nil || got != redirect {
				t.Fatalf("owner callback target = %q, %v", got, err)
			}
			if _, err := oauthstate.Consume(ctx, service.Redis, tc.method, "bind", owner.ID, state); !oauthstate.IsRejected(err) {
				t.Fatalf("binding state could be consumed twice: %v", err)
			}
		})
	}
	for _, method := range []string{"telegram", "apple"} {
		count := len(cache.Keys())
		if response, err := logic.BindOAuth(&types.BindOAuthRequest{Method: method, Redirect: "https://elsewhere.example.test/callback"}); response != nil || err == nil || len(cache.Keys()) != count {
			t.Fatalf("%s accepted a redirect outside the site's origin", method)
		}
	}
}
