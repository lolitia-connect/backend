package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestInterruptedRegistrationCanRetryWithoutAnOrphanedAccountOrTrial(t *testing.T) {
	for _, channel := range []struct {
		name, path, identity, fields, codePrefix string
	}{
		{"email", "/v1/auth/register", "reader@example.test", `"email":"reader@example.test"`, config.AuthCodeCacheKey},
		{"mobile", "/v1/auth/register/telephone", "+12025550123", `"telephone_area_code":"1","telephone":"2025550123"`, config.AuthCodeTelephoneCacheKey},
	} {
		t.Run(channel.name, func(t *testing.T) {
			f := newRouteFixture(t)
			ctx := t.Context()
			referrer := f.client.User.Create().SetPassword("fixture").SetReferCode("INVITER").SaveX(ctx)
			plan := f.client.Subscribe.Create().SetName("Welcome trial").SetTraffic(4096).SaveX(ctx)
			f.service.Config.JwtAuth.AccessExpire = 300
			f.service.Config.Invite.ForcedInvite = true
			f.service.Config.Email.EnableVerify = true
			f.service.Config.Mobile.Enable = true
			f.service.Config.Register.EnableTrial = true
			f.service.Config.Register.TrialSubscribe = plan.ID
			f.service.Config.Register.TrialTime, f.service.Config.Register.TrialTimeUnit = 2, "Hour"
			var blockedType string
			var blockedOp ent.Op
			var interrupted bool
			f.client.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
					if mutation.Type() == blockedType && mutation.Op().Is(blockedOp) {
						interrupted = true
						return nil, fmt.Errorf("registration write unavailable: %s", blockedType)
					}
					return next.Mutate(ctx, mutation)
				})
			})
			register := func(want uint32) json.RawMessage {
				t.Helper()
				key := fmt.Sprintf("%s:%s:%s", channel.codePrefix, constant.Register, channel.identity)
				require.NoError(t, f.service.Redis.Set(ctx, key, `{"code":"327451"}`, time.Minute).Err())
				body := `{` + channel.fields + `,"password":"signup-fixture","invite":"INVITER","code":"327451"}`
				return f.request(http.MethodPost, channel.path, "", body, want)
			}
			for _, write := range []struct {
				entity string
				op     ent.Op
			}{{ent.TypeUser, ent.OpCreate}, {ent.TypeUser, ent.OpUpdateOne}, {ent.TypeUserAuthMethod, ent.OpCreate}, {ent.TypeUserSubscribe, ent.OpCreate}} {
				blockedType, blockedOp, interrupted = write.entity, write.op, false
				require.Empty(t, register(xerr.ERROR))
				require.True(t, interrupted, "%s %s", write.entity, write.op)
				require.Equal(t, referrer.ID, f.client.User.Query().OnlyX(ctx).ID)
				require.Zero(t, f.client.UserAuthMethod.Query().CountX(ctx))
				require.Zero(t, f.client.UserSubscribe.Query().CountX(ctx))
				require.Zero(t, f.client.SystemLog.Query().CountX(ctx))
			}
			blockedType = ""
			var login types.LoginResponse
			require.NoError(t, json.Unmarshal(register(http.StatusOK), &login))
			var profile types.User
			require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/user/info", login.Token, "", http.StatusOK), &profile))
			identity := f.client.UserAuthMethod.Query().OnlyX(ctx)
			require.Equal(t, channel.identity, identity.AuthIdentifier)
			require.True(t, identity.Verified)
			require.Equal(t, profile.Id, identity.UserID)
			require.Equal(t, referrer.ID, f.client.User.GetX(ctx, profile.Id).RefererID)
			trial := f.client.UserSubscribe.Query().OnlyX(ctx)
			require.Equal(t, plan.ID, trial.SubscribeID)
			require.Equal(t, profile.Id, trial.UserID)
			require.Equal(t, plan.Traffic, trial.Traffic)
		})
	}
}

func TestChallengeEndpointsFollowThePublishedVerificationMethod(t *testing.T) {
	f := newRouteFixture(t)
	_, admin := f.account(true)
	imageSize := func(encoded string) image.Config {
		t.Helper()
		_, payload, ok := strings.Cut(encoded, ";base64,")
		require.True(t, ok)
		data, err := base64.StdEncoding.DecodeString(payload)
		require.NoError(t, err)
		size, _, err := image.DecodeConfig(bytes.NewReader(data))
		require.NoError(t, err)
		require.Positive(t, size.Width)
		require.Positive(t, size.Height)
		return size
	}
	for _, method := range []string{"local", "slider", "turnstile"} {
		payload := fmt.Sprintf(`{"captcha_type":%q,"turnstile_site_key":"public-challenge-key","turnstile_secret":"fixture-private-key"}`, method)
		f.request(http.MethodPut, "/v1/admin/system/verify_config", admin, payload, http.StatusOK)
		for _, endpoint := range []string{"/v1/auth/captcha", "/v1/auth/admin/captcha"} {
			raw := f.request(http.MethodPost, endpoint+"/generate", "", `{}`, http.StatusOK)
			require.NotContains(t, string(raw), "fixture-private-key")
			var challenge types.GenerateCaptchaResponse
			require.NoError(t, json.Unmarshal(raw, &challenge))
			require.Equal(t, method, challenge.Type)
			require.NotEmpty(t, challenge.Id)
			switch method {
			case "turnstile":
				require.Equal(t, "public-challenge-key", challenge.Id)
				require.Empty(t, challenge.Image)
			case "local":
				imageSize(challenge.Image)
				require.Empty(t, challenge.BlockImage)
			case "slider":
				background, tile := imageSize(challenge.Image), imageSize(challenge.BlockImage)
				require.Greater(t, background.Width, tile.Width)
				rejected := fmt.Sprintf(`{"id":%q,"x":-1,"y":-1,"trail":"[]"}`, challenge.Id)
				require.Empty(t, f.request(http.MethodPost, endpoint+"/slider/verify", "", rejected, xerr.VerifyCodeError))
			}
		}
	}
}

func TestPhoneRecoveryPreservesTrialAndRotatesTheCustomerSession(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	f.service.Config.JwtAuth.AccessExpire = 300
	f.service.Config.Mobile.Enable = true
	plan := f.client.Subscribe.Create().SetName("Phone trial").SetTraffic(2048).SaveX(ctx)
	f.service.Config.Register.EnableTrial = true
	f.service.Config.Register.TrialSubscribe = plan.ID
	f.service.Config.Register.TrialTime, f.service.Config.Register.TrialTimeUnit = 2, "Hour"
	const phone = "+12025550123"
	code := func(purpose, value string) {
		t.Helper()
		key := fmt.Sprintf("%s:%s:%s", config.AuthCodeTelephoneCacheKey, purpose, phone)
		require.NoError(t, f.service.Redis.Set(ctx, key, fmt.Sprintf(`{"code":%q}`, value), time.Minute).Err())
	}
	code(fmt.Sprint(constant.Register), "351820")
	var registered types.LoginResponse
	const fields = `"telephone_area_code":"1","telephone":"2025550123"`
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/auth/register/telephone", "", `{`+fields+`,"code":"351820","password":"registration-fixture"}`, http.StatusOK), &registered))
	identity := f.client.UserAuthMethod.Query().OnlyX(ctx)
	require.Equal(t, phone, identity.AuthIdentifier)
	trial := f.client.UserSubscribe.Query().OnlyX(ctx)
	require.Equal(t, identity.UserID, trial.UserID)
	require.Equal(t, plan.ID, trial.SubscribeID)
	require.EqualValues(t, 2048, trial.Traffic)
	require.WithinDuration(t, trial.StartTime.Add(2*time.Hour), trial.ExpireTime, time.Second)
	previous := f.client.User.GetX(ctx, identity.UserID).Password
	f.request(http.MethodPut, "/v1/admin/system/verify_config", operator, `{"captcha_type":"turnstile","turnstile_secret":"fixture-recovery","enable_user_reset_password_captcha":true}`, http.StatusOK)
	code(fmt.Sprint(constant.Security), "915273")
	accepted := false
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = providerTransport(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Host != "challenges.cloudflare.com" || req.URL.Path != "/turnstile/v0/siteverify" {
			return nil, fmt.Errorf("unexpected verifier request %s %s", req.Method, req.URL.Path)
		}
		if err := req.ParseMultipartForm(4096); err != nil {
			return nil, err
		}
		require.Equal(t, "fixture-recovery", req.FormValue("secret"))
		require.Equal(t, "fixture-proof", req.FormValue("response"))
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"success":%t}`, accepted)))}, nil
	})
	reset := func(proof string, expected uint32) json.RawMessage {
		t.Helper()
		return f.request(http.MethodPost, "/v1/auth/reset/telephone", "", `{`+fields+fmt.Sprintf(`,"password":"new-phone-fixture","cf_token":"fixture-proof","code":%q}`, proof), expected)
	}
	reset("915273", xerr.TooManyRequests)
	accepted = true
	reset("000000", xerr.VerifyCodeError)
	require.Equal(t, previous, f.client.User.GetX(ctx, identity.UserID).Password)
	const methods = "/v1/public/user/oauth_methods"
	before := f.request(http.MethodGet, methods, registered.Token, "", http.StatusOK)
	var recovered types.LoginResponse
	require.NoError(t, json.Unmarshal(reset("915273", http.StatusOK), &recovered))
	f.request(http.MethodGet, methods, registered.Token, "", xerr.InvalidAccess)
	require.JSONEq(t, string(before), string(f.request(http.MethodGet, methods, recovered.Token, "", http.StatusOK)))
	require.NotEqual(t, previous, f.client.User.GetX(ctx, identity.UserID).Password)
	require.Equal(t, trial.ID, f.client.UserSubscribe.Query().OnlyX(ctx).ID)
}

func TestEmailVerificationSettingTakesEffectOnNextRegistration(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	f.service.Config.JwtAuth.AccessExpire = 300
	method := f.client.AuthMethod.Create().SetMethod("email").SetConfig(`{}`).SetEnabled(false).SaveX(ctx)
	f.client.System.Create().SetCategory("ad").SetKey("WebAD").SetValue("false").SetType("bool").SetDesc("fixture").SaveX(ctx)

	public := func() types.GetGlobalConfigResponse {
		t.Helper()
		data := f.request(http.MethodGet, "/v1/common/site/config", "", "", http.StatusOK)
		require.NotContains(t, string(data), "mail-relay.example.test")
		var response types.GetGlobalConfigResponse
		require.NoError(t, json.Unmarshal(data, &response))
		return response
	}
	require.NotContains(t, public().OAuthMethods, "email")
	const configPath = "/v1/admin/auth-method/config"
	payload := fmt.Sprintf(`{"id":"%d","method":"email","enabled":true,"config":{"enable_verify":true,"platform":"smtp","platform_config":{"host":"mail-relay.example.test"}}}`, method.ID)
	f.request(http.MethodPut, configPath, operator, payload, http.StatusOK)
	var saved types.AuthMethodConfig
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, configPath+"?method=email", operator, "", http.StatusOK), &saved))
	require.Equal(t, method.ID, saved.Id)
	require.True(t, saved.Enabled)
	cfg := public()
	require.Contains(t, cfg.OAuthMethods, "email")
	require.True(t, cfg.Auth.Email.EnableVerify)

	const address = "verified@example.test"
	key := fmt.Sprintf("%s:%s:%s", config.AuthCodeCacheKey, constant.Register, address)
	require.NoError(t, f.service.Redis.Set(ctx, key, `{"code":"628391"}`, time.Minute).Err())
	register := func(code string, expected uint32) json.RawMessage {
		t.Helper()
		return f.request(http.MethodPost, "/v1/auth/register", "", fmt.Sprintf(`{"email":%q,"password":"registration-fixture","code":%q}`, address, code), expected)
	}
	register("wrong-code", xerr.VerifyCodeError)
	require.Zero(t, f.client.UserAuthMethod.Query().CountX(ctx))
	var login types.LoginResponse
	require.NoError(t, json.Unmarshal(register("628391", http.StatusOK), &login))
	require.NotEmpty(t, login.Token)
	var identities types.GetOAuthMethodsResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/user/oauth_methods", login.Token, "", http.StatusOK), &identities))
	require.Equal(t, []types.UserAuthMethod{{AuthType: "email", AuthIdentifier: address, Verified: true}}, identities.Methods)
}
