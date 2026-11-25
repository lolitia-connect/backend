package handler

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/auth"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

type providerTransport func(*http.Request) (*http.Response, error)

func (fn providerTransport) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestGitHubLoginUsesTheLastPublishedOperatorConfiguration(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	f.client.System.Create().SetCategory("ad").SetKey("WebAD").SetValue("false").SetType("bool").SetDesc("fixture").SaveX(ctx)
	method := f.client.AuthMethod.Create().SetMethod("github").SetEnabled(false).SetConfig(`{}`).SaveX(ctx)
	const configPath = "/v1/admin/auth-method/config"
	publicMethods := func() []string {
		t.Helper()
		data := f.request(http.MethodGet, "/v1/common/site/config", "", "", http.StatusOK)
		require.NotContains(t, string(data), "operator-only")
		var result types.GetGlobalConfigResponse
		require.NoError(t, json.Unmarshal(data, &result))
		return result.OAuthMethods
	}
	configure := func(client, callback string, want uint32) {
		t.Helper()
		body := fmt.Sprintf(`{"id":"%d","method":"github","enabled":true,"config":{"client_id":%q,"client_secret":"operator-only","redirect_url":%q}}`, method.ID, client, callback)
		f.request(http.MethodPut, configPath, operator, body, want)
	}
	checkLogin := func(client, callback string) {
		t.Helper()
		var login types.OAuthLoginResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/auth/oauth/login", "", `{"method":"github"}`, http.StatusOK), &login))
		authorize, err := url.Parse(login.Redirect)
		require.NoError(t, err)
		require.Equal(t, "github.com", authorize.Host)
		require.Equal(t, client, authorize.Query().Get("client_id"))
		require.Equal(t, callback, authorize.Query().Get("redirect_uri"))
		require.NotEmpty(t, authorize.Query().Get("state"))
		require.NotContains(t, login.Redirect, "operator-only")
	}
	const initial = "https://panel.example.test/auth"
	const replacement = "https://members.example.test/auth"
	require.NotContains(t, publicMethods(), "github")
	configure("original-client", initial, http.StatusOK)
	require.Contains(t, publicMethods(), "github")
	checkLogin("original-client", initial)
	blocked := true
	f.client.AuthMethod.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if blocked {
				return nil, fmt.Errorf("provider configuration is read-only")
			}
			return next.Mutate(ctx, mutation)
		})
	})
	configure("replacement-client", replacement, xerr.DatabaseQueryError)
	checkLogin("original-client", initial)
	blocked = false
	configure("replacement-client", replacement, http.StatusOK)
	checkLogin("replacement-client", replacement)
	for _, enabled := range []bool{false, true} {
		body := fmt.Sprintf(`{"id":"%d","method":"github","enabled":%t}`, method.ID, enabled)
		f.request(http.MethodPut, configPath, operator, body, http.StatusOK)
		if !enabled {
			require.NotContains(t, publicMethods(), "github")
		} else {
			require.Contains(t, publicMethods(), "github")
			checkLogin("replacement-client", replacement)
		}
	}
}

func TestAppleFormPostReturnsCustomerToPanelAndExchangesTheSameRedirectURI(t *testing.T) {
	const site = "https://panel.example.test"
	const api = "https://api.example.test"
	const callback = "/v1/auth/oauth/callback/apple"
	const clientID = "test.panel.web"
	const identity = "apple-customer-42"
	f := newRouteFixture(t)
	f.service.Config.Site.Host = site
	f.service.Config.JwtAuth.AccessExpire = 300
	owner, session := f.account(false)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	config := auth.AppleAuthConfig{TeamID: "team", KeyID: "key", ClientId: clientID, RedirectURL: api,
		ClientSecret: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))}
	f.client.AuthMethod.Create().SetMethod("apple").SetEnabled(true).SetConfig(config.Marshal()).SaveX(t.Context())
	idToken, err := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": "https://appleid.apple.com", "aud": clientID, "sub": identity, "email": "customer@example.test",
	}).SignedString(key)
	require.NoError(t, err)
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = providerTransport(func(req *http.Request) (*http.Response, error) {
		if err := req.ParseForm(); err != nil {
			return nil, err
		}
		if req.Method != http.MethodPost || req.URL.String() != "https://appleid.apple.com/auth/token" ||
			req.Form.Get("client_id") != clientID || req.Form.Get("redirect_uri") != api+callback || req.Form.Get("grant_type") != "authorization_code" || req.Form.Get("code") == "" {
			return nil, fmt.Errorf("unexpected Apple code exchange at %s with redirect %q", req.URL, req.Form.Get("redirect_uri"))
		}
		if _, err := jwt.Parse(req.Form.Get("client_secret"), func(*jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithIssuer("team"), jwt.WithSubject(clientID), jwt.WithAudience("https://appleid.apple.com")); err != nil {
			return nil, err
		}
		body := fmt.Sprintf(`{"id_token":%q,"access_token":"opaque-access-token","token_type":"Bearer"}`, idToken)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {"application/json"}}}, nil
	})
	complete := func(startPath, endPath, session, code string) json.RawMessage {
		t.Helper()
		var start types.OAuthLoginResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodPost, startPath, session, `{"method":"apple","redirect":"`+site+`/account?tab=devices"}`, http.StatusOK), &start))
		authorize, err := url.Parse(start.Redirect)
		require.NoError(t, err)
		require.Equal(t, "appleid.apple.com", authorize.Host)
		require.Equal(t, api+callback, authorize.Query().Get("redirect_uri"))
		require.Equal(t, "form_post", authorize.Query().Get("response_mode"))
		state := authorize.Query().Get("state")
		require.NotEmpty(t, state)
		form := url.Values{"state": {state}, "code": {code}}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, callback, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, req)
		require.Equal(t, http.StatusFound, w.Code)
		redirect, err := url.Parse(w.Header().Get("Location"))
		require.NoError(t, err)
		require.Equal(t, site+"/account", redirect.Scheme+"://"+redirect.Host+redirect.Path)
		require.Equal(t, url.Values{"tab": {"devices"}, "method": {"apple"}, "code": {code}, "state": {state}}, redirect.Query())
		payload := fmt.Sprintf(`{"method":"apple","callback":{"state":%q,"code":%q}}`, state, code)
		return f.request(http.MethodPost, endPath, session, payload, http.StatusOK)
	}
	complete("/v1/public/user/bind_oauth", "/v1/public/user/bind_oauth/callback", session, "bind-code")
	binding := f.client.UserAuthMethod.Query().OnlyX(t.Context())
	require.Equal(t, owner.ID, binding.UserID)
	require.Equal(t, identity, binding.AuthIdentifier)
	var login types.LoginResponse
	require.NoError(t, json.Unmarshal(complete("/v1/auth/oauth/login", "/v1/auth/oauth/login/token", "", "sign-in-code"), &login))
	var profile types.User
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/user/info", login.Token, "", http.StatusOK), &profile))
	require.Equal(t, owner.ID, profile.Id)
}

func TestInvitedGitHubSignupKeepsItsTicketUntilTheTrialIsGranted(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	const redirect = "https://panel.example.test/account"
	f.service.Config.Site.Host = "https://panel.example.test"
	f.service.Config.JwtAuth.AccessExpire = 300
	f.service.Config.Invite.ForcedInvite = true
	f.service.Config.Register.EnableTrial = true
	f.service.Config.Register.TrialSubscribe = 999
	f.service.Config.Register.TrialTimeUnit = "Day"
	f.service.Config.Register.TrialTime = 2
	referrer := f.client.User.Create().SetReferCode("INVITER").SetPassword("fixture").SaveX(ctx)
	f.client.AuthMethod.Create().SetMethod("github").SetEnabled(true).SetConfig(`{"client_id":"fixture","client_secret":"fixture","redirect_url":"` + redirect + `"}`).SaveX(ctx)
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = providerTransport(func(req *http.Request) (*http.Response, error) {
		var body string
		switch {
		case req.Method == http.MethodPost && req.URL.String() == "https://github.com/login/oauth/access_token":
			body = `{"access_token":"signup-token","token_type":"Bearer"}`
		case req.Method == http.MethodGet && req.URL.String() == "https://api.github.com/user" && req.Header.Get("Authorization") == "Bearer signup-token":
			body = `{"id":807,"login":"new-member","email":null}`
		case req.Method == http.MethodGet && req.URL.String() == "https://api.github.com/user/emails" && req.Header.Get("Authorization") == "Bearer signup-token":
			body = `[{"email":"secondary@example.test","verified":true},{"email":"member@example.test","primary":true,"verified":true}]`
		default:
			return nil, fmt.Errorf("unexpected GitHub signup request %s %s", req.Method, req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	var start types.OAuthLoginResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/auth/oauth/login", "", `{"method":"github","redirect":"`+redirect+`"}`, http.StatusOK), &start))
	location, err := url.Parse(start.Redirect)
	require.NoError(t, err)
	callback := fmt.Sprintf(`{"method":"github","callback":{"state":%q,"code":"signup"}}`, location.Query().Get("state"))
	var pending types.LoginResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/auth/oauth/login/token", "", callback, http.StatusOK), &pending))
	require.True(t, pending.InviteRequired)
	require.False(t, pending.EmailRequired)
	require.Empty(t, pending.Token)
	require.NotEmpty(t, pending.OAuthToken)
	register := func(code string, want uint32) json.RawMessage {
		t.Helper()
		return f.request(http.MethodPost, "/v1/auth/oauth/register", "", fmt.Sprintf(`{"oauth_token":%q,"invite":%q}`, pending.OAuthToken, code), want)
	}
	register("unknown", xerr.InviteCodeError)
	register(referrer.ReferCode, xerr.ERROR)
	require.Equal(t, 1, f.client.User.Query().CountX(ctx))
	require.Zero(t, f.client.UserAuthMethod.Query().CountX(ctx))
	trial := f.client.Subscribe.Create().SetName("Welcome trial").SetTraffic(4096).SaveX(ctx)
	f.service.Config.Register.TrialSubscribe = trial.ID
	var signedIn types.LoginResponse
	require.NoError(t, json.Unmarshal(register(referrer.ReferCode, http.StatusOK), &signedIn))
	var identities types.GetOAuthMethodsResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/user/oauth_methods", signedIn.Token, "", http.StatusOK), &identities))
	require.ElementsMatch(t, []types.UserAuthMethod{{AuthType: "email", AuthIdentifier: "member@example.test", Verified: true}, {AuthType: "github", AuthIdentifier: "807", Verified: true}}, identities.Methods)
	register(referrer.ReferCode, xerr.InvalidAccess)
	grant := f.client.UserSubscribe.Query().OnlyX(ctx)
	require.Equal(t, trial.ID, grant.SubscribeID)
	require.Equal(t, trial.Traffic, grant.Traffic)
	require.True(t, grant.StartTime.AddDate(0, 0, 2).Equal(grant.ExpireTime))
	require.Equal(t, referrer.ID, f.client.User.GetX(ctx, grant.UserID).RefererID)
}

func TestOAuthLinkedIdentityCanSignInToTheSameAccount(t *testing.T) {
	const redirect = "https://panel.example.test/account/callback"
	const bindPath = "/v1/public/user/bind_oauth"
	const loginPath = "/v1/auth/oauth/login"
	for _, provider := range []struct {
		name, authHost, tokenHost, profileHost, profilePath, profile, openID string
	}{
		{"google", "accounts.google.com", "oauth2.googleapis.com", "www.googleapis.com", "/oauth2/v2/userinfo", `{"id":"g-42","email":"reader@example.test","verified_email":"true"}`, "g-42"},
		{"github", "github.com", "github.com", "api.github.com", "/user", `{"id":42,"login":"reader","email":"reader@example.test"}`, "42"},
		{"facebook", "www.facebook.com", "graph.facebook.com", "graph.facebook.com", "/v22.0/me", `{"id":"fb-42","name":"Reader","email":"reader@example.test"}`, "fb-42"},
	} {
		t.Run(provider.name, func(t *testing.T) {
			f := newRouteFixture(t)
			f.service.Config.JwtAuth.AccessExpire = 300
			account, ownerSession := f.account(false)
			ctx := t.Context()
			settings := f.client.AuthMethod.Create().SetMethod(provider.name).SetEnabled(true).
				SetConfig(fmt.Sprintf(`{"client_id":"fixture-client","client_secret":"fixture-secret","redirect_url":%q}`, redirect)).SaveX(ctx)
			configOffline := false
			f.client.AuthMethod.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
				return ent.QuerierFunc(func(ctx context.Context, query ent.Query) (ent.Value, error) {
					if configOffline {
						return nil, fmt.Errorf("provider configuration unavailable")
					}
					return next.Query(ctx, query)
				})
			}))

			var exchanges, profiles, rejected int
			var unavailableMethod string
			previous := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previous })
			http.DefaultTransport = providerTransport(func(req *http.Request) (*http.Response, error) {
				var body string
				switch {
				case req.Method == http.MethodPost && req.URL.Hostname() == provider.tokenHost:
					if err := req.ParseForm(); err != nil {
						return nil, err
					}
					if req.Form.Get("code") != "fixture-code" || req.Form.Get("redirect_uri") != redirect {
						return nil, fmt.Errorf("incorrect OAuth authorization code or callback URL")
					}
					exchanges++
					body = `{"access_token":"fixture-access","token_type":"Bearer","expires_in":600}`
				case req.Method == http.MethodGet && req.URL.Hostname() == provider.profileHost && req.URL.Path == provider.profilePath:
					if req.Header.Get("Authorization") != "Bearer fixture-access" {
						return nil, fmt.Errorf("missing provider bearer credential")
					}
					profiles++
					body = provider.profile
				default:
					return nil, fmt.Errorf("unexpected provider request: %s %s%s", req.Method, req.URL.Host, req.URL.Path)
				}
				status := http.StatusOK
				if req.Method == unavailableMethod {
					rejected++
					status = http.StatusServiceUnavailable
					body = `{"error":"temporarily_unavailable"}`
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})

			start := func(path, session string) string {
				t.Helper()
				var response types.OAuthLoginResponse
				require.NoError(t, json.Unmarshal(f.request(http.MethodPost, path, session, fmt.Sprintf(`{"method":%q,"redirect":%q}`, provider.name, redirect), http.StatusOK), &response))
				location, err := url.Parse(response.Redirect)
				require.NoError(t, err)
				require.Equal(t, provider.authHost, location.Hostname())
				require.Equal(t, redirect, location.Query().Get("redirect_uri"))
				state := location.Query().Get("state")
				require.NotEmpty(t, state)
				return fmt.Sprintf(`{"method":%q,"callback":{"state":%q,"code":"fixture-code"}}`, provider.name, state)
			}
			checkOutage := func(beginPath, endPath, session string) {
				t.Helper()
				begin := fmt.Sprintf(`{"method":%q,"redirect":%q}`, provider.name, redirect)
				for _, fault := range []struct {
					name string
					code uint32
				}{{"unavailable", xerr.DatabaseQueryError}, {"unreadable", xerr.ERROR}} {
					payload := start(beginPath, session)
					beforeExchange, beforeProfile := exchanges, profiles
					if fault.name == "unavailable" {
						configOffline = true
					} else {
						f.client.AuthMethod.UpdateOneID(settings.ID).SetConfig(`{"client_id":`).ExecX(ctx)
					}
					require.Empty(t, f.request(http.MethodPost, beginPath, session, begin, xerr.ERROR))
					require.Empty(t, f.request(http.MethodPost, endPath, session, payload, fault.code))
					configOffline = false
					f.client.AuthMethod.UpdateOneID(settings.ID).SetConfig(settings.Config).ExecX(ctx)
					require.Empty(t, f.request(http.MethodPost, endPath, session, payload, xerr.ERROR))
					require.Equal(t, beforeExchange, exchanges, fault.name)
					require.Equal(t, beforeProfile, profiles, fault.name)
				}
				for _, method := range []string{http.MethodPost, http.MethodGet} {
					unavailableMethod = method
					before := rejected
					payload := start(beginPath, session)
					require.Empty(t, f.request(http.MethodPost, endPath, session, payload, xerr.ERROR))
					require.Greater(t, rejected, before)
				}
				unavailableMethod = ""
			}
			checkOutage(bindPath, bindPath+"/callback", ownerSession)
			require.Zero(t, f.client.UserAuthMethod.Query().CountX(ctx))
			beforeBind := exchanges
			bind := start(bindPath, ownerSession)
			f.request(http.MethodPost, loginPath+"/token", "", bind, xerr.ERROR)
			require.Equal(t, beforeBind, exchanges)
			f.request(http.MethodPost, bindPath+"/callback", ownerSession, bind, http.StatusOK)
			binding := f.client.UserAuthMethod.Query().OnlyX(ctx)
			require.Equal(t, account.ID, binding.UserID)
			require.Equal(t, provider.openID, binding.AuthIdentifier)
			require.Equal(t, provider.name, binding.AuthType)
			require.True(t, binding.Verified)

			checkOutage(loginPath, loginPath+"/token", "")
			require.Equal(t, binding.ID, f.client.UserAuthMethod.Query().OnlyX(ctx).ID)
			beforeLogin, beforeProfile := exchanges, profiles
			login := start(loginPath, "")
			var result types.LoginResponse
			require.NoError(t, json.Unmarshal(f.request(http.MethodPost, loginPath+"/token", "", login, http.StatusOK), &result))
			require.NotEmpty(t, result.Token)
			var methods types.GetOAuthMethodsResponse
			require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/user/oauth_methods", result.Token, "", http.StatusOK), &methods))
			require.Equal(t, []types.UserAuthMethod{{AuthType: provider.name, AuthIdentifier: provider.openID, Verified: true}}, methods.Methods)
			f.request(http.MethodPost, loginPath+"/token", "", login, xerr.ERROR)
			require.Equal(t, beforeLogin+1, exchanges)
			require.Equal(t, beforeProfile+1, profiles)
			require.Equal(t, 1, f.client.User.Query().CountX(ctx))
		})
	}
}
