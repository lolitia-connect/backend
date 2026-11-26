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

	framework "entgo.io/ent"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/systemlog"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
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

func TestPasswordRecoveryKeepsTheOldSessionUntilCredentialsAreSaved(t *testing.T) {
	for _, tc := range []struct {
		path, method, identity, fields, prefix string
		writeError                             uint32
	}{
		{"/v1/auth/reset", "email", "reader@example.test", `"email":"reader@example.test"`, config.AuthCodeCacheKey, xerr.DatabaseUpdateError},
		{"/v1/auth/admin/reset", "email", "operator@example.test", `"email":"operator@example.test"`, config.AuthCodeCacheKey, xerr.DatabaseUpdateError},
		{"/v1/auth/reset/telephone", "mobile", "+12025550123", `"telephone_area_code":"1","telephone":"2025550123"`, config.AuthCodeTelephoneCacheKey, xerr.ERROR},
	} {
		t.Run(tc.path, func(t *testing.T) {
			f := newRouteFixture(t)
			ctx := t.Context()
			f.service.Config.JwtAuth.AccessExpire = 300
			f.service.Config.Mobile.Enable = true
			owner, previous := f.account(tc.path == "/v1/auth/admin/reset")
			f.client.UserAuthMethod.Create().SetUserID(owner.ID).SetAuthType(tc.method).SetAuthIdentifier(tc.identity).SetVerified(true).SaveX(ctx)
			var unavailable string
			f.client.Intercept(framework.InterceptFunc(func(next framework.Querier) framework.Querier {
				return framework.QuerierFunc(func(ctx context.Context, q framework.Query) (framework.Value, error) {
					if framework.QueryFromContext(ctx).Type == unavailable {
						return nil, fmt.Errorf("%s temporarily unavailable", unavailable)
					}
					return next.Query(ctx, q)
				})
			}))
			f.client.User.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
					if unavailable == "password write" {
						return nil, fmt.Errorf("account storage is read-only")
					}
					return next.Mutate(ctx, m)
				})
			})
			reset := func(want uint32) json.RawMessage {
				t.Helper()
				key := fmt.Sprintf("%s:%s:%s", tc.prefix, constant.Security, tc.identity)
				require.NoError(t, f.service.Redis.Set(ctx, key, `{"code":"681452"}`, time.Minute).Err())
				body := `{` + tc.fields + `,"password":"recovered-password","code":"681452","identifier":"recovery-phone"}`
				return f.request(http.MethodPost, tc.path, "", body, want)
			}
			const profile = "/v1/public/user/info"
			for _, blocked := range []string{ent.TypeSystem, ent.TypeUserAuthMethod, ent.TypeUser, "password write"} {
				unavailable = blocked
				want := xerr.DatabaseQueryError
				if blocked == "password write" {
					want = tc.writeError
				}
				require.Empty(t, reset(want), blocked)
				unavailable = ""
				require.Equal(t, owner.Password, f.client.User.GetX(ctx, owner.ID).Password)
				require.Zero(t, f.client.UserDevice.Query().CountX(ctx))
				require.Zero(t, f.client.SystemLog.Query().CountX(ctx))
				f.request(http.MethodGet, profile, previous, "", http.StatusOK)
			}
			var recovered types.LoginResponse
			require.NoError(t, json.Unmarshal(reset(http.StatusOK), &recovered))
			f.request(http.MethodGet, profile, previous, "", xerr.InvalidAccess)
			var account types.User
			require.NoError(t, json.Unmarshal(f.request(http.MethodGet, profile, recovered.Token, "", http.StatusOK), &account))
			require.Equal(t, owner.ID, account.Id)
			require.True(t, tool.VerifyPassWord("recovered-password", f.client.User.GetX(ctx, owner.ID).Password))
			device := f.client.UserDevice.Query().OnlyX(ctx)
			require.Equal(t, owner.ID, device.UserID)
			require.Equal(t, "recovery-phone", device.Identifier)
			var audit log.Login
			require.NoError(t, json.Unmarshal([]byte(f.client.SystemLog.Query().Where(systemlog.Type(log.TypeLogin.Uint8()), systemlog.ObjectID(owner.ID)).OnlyX(ctx).Content), &audit))
			require.True(t, audit.Success)
			require.Equal(t, tc.method, audit.Method)
		})
	}
}

func TestLoginRetriesAfterAccountStorageRecovers(t *testing.T) {
	for _, channel := range []struct{ path, method, identity, fields string }{
		{"/v1/auth/login/telephone", "mobile", "+12025550123", `"telephone_area_code":"1","telephone":"2025550123"`},
		{"/v1/auth/login", "email", "member@example.test", `"email":"member@example.test"`},
		{"/v1/auth/admin/login", "email", "operator@example.test", `"email":"operator@example.test"`},
	} {
		t.Run(channel.path, func(t *testing.T) {
			f := newRouteFixture(t)
			ctx := t.Context()
			f.service.Config.Mobile.Enable = true
			f.service.Config.JwtAuth.AccessExpire = 300
			owner := f.client.User.Create().SetPassword(tool.EncodePassWord("login-fixture")).SetAlgo(tool.PasswordAlgoArgon2id).
				SetEnable(true).SetIsAdmin(channel.path == "/v1/auth/admin/login").SaveX(ctx)
			f.client.UserAuthMethod.Create().SetUserID(owner.ID).SetAuthType(channel.method).SetAuthIdentifier(channel.identity).SetVerified(true).SaveX(ctx)
			var reads, unavailableAt int
			f.client.User.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
				return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
					reads++
					if reads == unavailableAt {
						return nil, fmt.Errorf("account temporarily unavailable")
					}
					return next.Query(ctx, q)
				})
			}))
			login := func(want uint32) json.RawMessage {
				t.Helper()
				reads = 0
				return f.request(http.MethodPost, channel.path, "", `{`+channel.fields+`,"password":"login-fixture"}`, want)
			}
			unavailableAt = 1
			require.Empty(t, login(xerr.DatabaseQueryError))
			require.Zero(t, f.client.SystemLog.Query().CountX(ctx))
			unavailableAt = 2
			require.Empty(t, login(xerr.InvalidAccess))
			audits := f.client.SystemLog.Query().Where(systemlog.ObjectID(owner.ID), systemlog.Type(log.TypeLogin.Uint8()))
			var denied log.Login
			require.NoError(t, json.Unmarshal([]byte(audits.Clone().OnlyX(ctx).Content), &denied))
			require.False(t, denied.Success)
			require.Equal(t, channel.method, denied.Method)
			unavailableAt = 0
			var issued types.LoginResponse
			require.NoError(t, json.Unmarshal(login(http.StatusOK), &issued))
			var profile types.User
			require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/user/info", issued.Token, "", http.StatusOK), &profile))
			require.Equal(t, owner.ID, profile.Id)
			require.Equal(t, 2, audits.Clone().CountX(ctx))
			var accepted log.Login
			require.NoError(t, json.Unmarshal([]byte(audits.Order(ent.Desc(systemlog.FieldID)).FirstX(ctx).Content), &accepted))
			require.True(t, accepted.Success)
		})
	}
}

func TestLegacyPasswordUpgradesOnTheNextSuccessfulWrite(t *testing.T) {
	const password = "legacy-account-fixture"
	legacy, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	for _, channel := range []struct{ method, identity, path, fields string }{
		{"email", "member@example.test", "/v1/auth/login", `"email":"member@example.test"`},
		{"mobile", "+12025550123", "/v1/auth/login/telephone", `"telephone_area_code":"1","telephone":"2025550123"`},
	} {
		t.Run(channel.method, func(t *testing.T) {
			f := newRouteFixture(t)
			ctx := t.Context()
			f.service.Config.JwtAuth.AccessExpire = 300
			f.service.Config.Mobile.Enable = true
			owner := f.client.User.Create().SetPassword(string(legacy)).SetAlgo("bcrypt").SetSalt("imported").SetEnable(true).SaveX(ctx)
			f.client.UserAuthMethod.Create().SetUserID(owner.ID).SetAuthType(channel.method).SetAuthIdentifier(channel.identity).SetVerified(true).SaveX(ctx)
			readOnly := true
			var writes int
			f.client.User.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
					if _, changed := m.(*ent.UserMutation).Password(); changed {
						writes++
						if readOnly {
							return nil, fmt.Errorf("account storage temporarily read-only")
						}
					}
					return next.Mutate(ctx, m)
				})
			})
			login := func(secret string, want uint32) json.RawMessage {
				t.Helper()
				return f.request(http.MethodPost, channel.path, "", fmt.Sprintf(`{%s,"password":%q}`, channel.fields, secret), want)
			}
			checkSession := func(raw json.RawMessage) {
				t.Helper()
				var issued types.LoginResponse
				require.NoError(t, json.Unmarshal(raw, &issued))
				var profile types.User
				require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/user/info", issued.Token, "", http.StatusOK), &profile))
				require.Equal(t, owner.ID, profile.Id)
			}
			login("incorrect-password", xerr.UserPasswordError)
			require.Zero(t, writes)
			first := login(password, http.StatusOK)
			checkSession(first)
			require.Equal(t, 1, writes)
			require.Equal(t, owner.Password, f.client.User.GetX(ctx, owner.ID).Password)
			readOnly = false
			checkSession(login(password, http.StatusOK))
			updated := f.client.User.GetX(ctx, owner.ID)
			require.Equal(t, tool.PasswordAlgoArgon2id, updated.Algo)
			require.Empty(t, updated.Salt)
			require.True(t, tool.VerifyPassWord(password, updated.Password))
			checkSession(first)
			checkSession(login(password, http.StatusOK))
			require.Equal(t, 2, writes)
			require.Equal(t, updated.Password, f.client.User.GetX(ctx, owner.ID).Password)
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

func TestChallengeProtectedLoginsRecoverWithoutBindingUnverifiedDevices(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	f.service.Config.JwtAuth.AccessExpire = 300
	f.service.Config.Mobile.Enable = true
	const password = "verified-login-fixture"
	const path = "/v1/admin/system/verify_config"
	configure := func(method string) {
		t.Helper()
		f.request(http.MethodPut, path, operator, fmt.Sprintf(`{"captcha_type":%q,"enable_user_login_captcha":true,"enable_admin_login_captcha":true,"turnstile_secret":"login-test-key"}`, method), http.StatusOK)
	}
	var unavailable bool
	f.client.System.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
			if unavailable {
				return nil, fmt.Errorf("verification settings offline")
			}
			return next.Query(ctx, q)
		})
	}))
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = providerTransport(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != "https://challenges.cloudflare.com/turnstile/v0/siteverify" {
			return nil, fmt.Errorf("unexpected verification request to %s", req.URL.Host)
		}
		if err := req.ParseMultipartForm(4096); err != nil {
			return nil, err
		}
		if req.FormValue("secret") != "login-test-key" {
			return nil, fmt.Errorf("wrong verifier configuration")
		}
		if req.FormValue("response") == "outage" {
			return nil, fmt.Errorf("verification unavailable")
		}
		body := fmt.Sprintf(`{"success":%t}`, req.FormValue("response") == "accepted")
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	for _, channel := range []struct {
		method, path, identity, fields string
		admin                          bool
	}{
		{"email", "/v1/auth/login", "member@example.test", `"email":"member@example.test"`, false},
		{"mobile", "/v1/auth/login/telephone", "+12025550124", `"telephone_area_code":"1","telephone":"2025550124"`, false},
		{"email", "/v1/auth/admin/login", "admin@example.test", `"email":"admin@example.test"`, true},
	} {
		t.Run(channel.path, func(t *testing.T) {
			fixture := *f
			fixture.t = t
			account := f.client.User.Create().SetPassword(tool.EncodePassWord(password)).SetAlgo(tool.PasswordAlgoArgon2id).SetIsAdmin(channel.admin).SetEnable(true).SaveX(ctx)
			f.client.UserAuthMethod.Create().SetUserID(account.ID).SetAuthType(channel.method).SetAuthIdentifier(channel.identity).SetVerified(true).SaveX(ctx)
			device := fmt.Sprintf("workstation-%d", account.ID)
			login := func(proof string, want uint32) json.RawMessage {
				t.Helper()
				body := fmt.Sprintf(`{%s,"password":%q,"identifier":%q,"cf_token":%q}`, channel.fields, password, device, proof)
				return fixture.request(http.MethodPost, channel.path, "", body, want)
			}
			before := f.client.UserDevice.Query().CountX(ctx)
			configure("local")
			unavailable = true
			require.Empty(t, login("", xerr.DatabaseQueryError))
			unavailable = false
			require.Empty(t, login("", xerr.VerifyCodeError))
			configure("slider")
			require.Empty(t, login("", xerr.VerifyCodeError))
			configure("turnstile")
			for _, proof := range []string{"", "outage", "declined"} {
				require.Empty(t, login(proof, xerr.VerifyCodeError))
			}
			require.Equal(t, before, f.client.UserDevice.Query().CountX(ctx))
			var signed types.LoginResponse
			require.NoError(t, json.Unmarshal(login("accepted", http.StatusOK), &signed))
			var me types.User
			require.NoError(t, json.Unmarshal(fixture.request(http.MethodGet, "/v1/public/user/info", signed.Token, "", http.StatusOK), &me))
			require.Equal(t, account.ID, me.Id)
			if !channel.admin {
				var devices types.GetDeviceListResponse
				require.NoError(t, json.Unmarshal(fixture.request(http.MethodGet, "/v1/public/user/devices", signed.Token, "", http.StatusOK), &devices))
				require.Len(t, devices.List, 1)
				require.Equal(t, device, devices.List[0].Identifier)
			}
		})
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
	lookup := func(want bool) {
		t.Helper()
		var result types.TelephoneCheckUserResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/auth/check/telephone?telephone_area_code=1&telephone=2025550123", "", "", http.StatusOK), &result))
		require.Equal(t, want, result.Exist)
	}
	lookup(false)
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
	lookup(true)
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

func TestLoginMethodsWithEmptySettingsCanBeConfiguredAndToggled(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	const path = "/v1/admin/auth-method/config"
	for _, tc := range []struct {
		method, field string
		value         any
	}{
		{"apple", "team_id", "TEST-TEAM"}, {"google", "client_id", "web-client"},
		{"github", "redirect_url", "https://portal.example.test"}, {"facebook", "client_id", "member-app"},
		{"telegram", "webhook_domain", "https://notify.example.test"}, {"device", "show_ads", true},
		{"email", "verify_email_subject", "Member verification"}, {"mobile", "enable_whitelist", true},
	} {
		t.Run(tc.method, func(t *testing.T) {
			fixture := *f
			fixture.t = t
			row := f.client.AuthMethod.Create().SetMethod(tc.method).SetEnabled(false).SetConfig("").SaveX(ctx)
			toggle := func(enabled bool) types.AuthMethodConfig {
				t.Helper()
				body := fmt.Sprintf(`{"id":"%d","method":%q,"enabled":%t}`, row.ID, tc.method, enabled)
				var result types.AuthMethodConfig
				require.NoError(t, json.Unmarshal(fixture.request(http.MethodPut, path, operator, body, http.StatusOK), &result))
				return result
			}
			initialized := toggle(true)
			require.Equal(t, row.ID, initialized.Id)
			require.True(t, initialized.Enabled)
			require.Contains(t, initialized.Config, tc.field)
			settings := map[string]any{tc.field: tc.value}
			body, err := json.Marshal(types.UpdateAuthMethodConfigRequest{Id: row.ID, Method: tc.method, Config: settings})
			require.NoError(t, err)
			fixture.request(http.MethodPut, path, operator, string(body), http.StatusOK)
			disabled := toggle(false)
			require.False(t, disabled.Enabled)
			var reloaded types.AuthMethodConfig
			require.NoError(t, json.Unmarshal(fixture.request(http.MethodGet, path+"?method="+tc.method, operator, "", http.StatusOK), &reloaded))
			require.Equal(t, disabled, reloaded)
			require.Equal(t, tc.value, reloaded.Config.(map[string]any)[tc.field])
			var stored map[string]any
			require.NoError(t, json.Unmarshal([]byte(f.client.AuthMethod.GetX(ctx, row.ID).Config), &stored))
			require.Equal(t, tc.value, stored[tc.field])
		})
	}
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
