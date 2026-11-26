package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestCurrentOperatorReadsTheirOwnWalletAcrossSessionSwitches(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	first, firstSession := f.account(true)
	second, secondSession := f.account(true)
	_, memberSession := f.account(false)
	firstWallet := f.client.UserWallet.Create().SetUserID(first.ID).SetBalance(1250).SetGiftAmount(35).SetCommission(400).SaveX(ctx)
	secondWallet := f.client.UserWallet.Create().SetUserID(second.ID).SetBalance(70).SetGiftAmount(200).SetCommission(18).SaveX(ctx)
	const current = "/v1/admin/user/current"
	require.Empty(t, f.request(http.MethodGet, current, memberSession, "", xerr.InvalidAccess))
	check := func(session string, wallet *ent.UserWallet) {
		t.Helper()
		var profile types.User
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, current, session, "", http.StatusOK), &profile))
		require.Equal(t, wallet.UserID, profile.Id)
		require.True(t, profile.IsAdmin)
		require.Equal(t, wallet.Balance, profile.Balance)
		require.Equal(t, wallet.GiftAmount, profile.GiftAmount)
		require.Equal(t, wallet.Commission, profile.Commission)
	}
	check(firstSession, firstWallet)
	check(secondSession, secondWallet)
	updated := f.client.UserWallet.UpdateOneID(firstWallet.ID).AddBalance(275).SaveX(ctx)
	check(firstSession, updated)
	check(secondSession, secondWallet)
}

func TestMemberCanVerifyAnEmailAndReplaceThePasswordUsedForLogin(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	f.service.Config.JwtAuth.AccessExpire = 300
	owner, session := f.account(false)
	const email = "account@example.test"
	const oldPassword, newPassword = "initial-account-password", "replacement-password"
	original := tool.EncodePassWord(oldPassword)
	f.client.User.UpdateOneID(owner.ID).SetPassword(original).SetAlgo(tool.PasswordAlgoArgon2id).ExecX(ctx)
	identity := f.client.UserAuthMethod.Create().SetUserID(owner.ID).SetAuthType("email").SetAuthIdentifier(email).SetVerified(false).SaveX(ctx)
	provider := f.client.UserAuthMethod.Create().SetUserID(owner.ID).SetAuthType("github").SetAuthIdentifier("member-404").SetVerified(true).SaveX(ctx)
	const member = "/v1/public/user"
	proofKey := fmt.Sprintf("%s:%s:%s", config.AuthCodeCacheKey, constant.Security, email)
	require.NoError(t, f.service.Redis.Set(ctx, proofKey, `{"code":"327451"}`, time.Minute).Err())
	verify := func(code string, want uint32) {
		t.Helper()
		f.request(http.MethodPost, member+"/verify_email", session, fmt.Sprintf(`{"email":%q,"code":%q}`, email, code), want)
	}
	verify("000000", xerr.VerifyCodeError)
	require.False(t, f.client.UserAuthMethod.GetX(ctx, identity.ID).Verified)
	verify("327451", http.StatusOK)
	require.True(t, f.client.UserAuthMethod.GetX(ctx, identity.ID).Verified)
	verify("327451", xerr.VerifyCodeError)
	f.request(http.MethodPost, member+"/unbind_oauth", session, `{"method":"email"}`, xerr.InvalidParams)
	f.request(http.MethodPost, member+"/unbind_oauth", session, `{"method":"github"}`, http.StatusOK)
	require.NotEqual(t, identity.ID, provider.ID)
	require.Equal(t, identity.ID, f.client.UserAuthMethod.Query().OnlyX(ctx).ID)
	change := func(old string, want uint32) {
		t.Helper()
		f.request(http.MethodPut, member+"/password", session, fmt.Sprintf(`{"old_password":%q,"password":%q}`, old, newPassword), want)
	}
	change("incorrect-password", xerr.UserPasswordError)
	require.Equal(t, original, f.client.User.GetX(ctx, owner.ID).Password)
	change(oldPassword, http.StatusOK)
	f.request(http.MethodGet, member+"/info", session, "", xerr.InvalidAccess)
	login := func(password string, want uint32) json.RawMessage {
		t.Helper()
		return f.request(http.MethodPost, "/v1/auth/login", "", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password), want)
	}
	login(oldPassword, xerr.UserPasswordError)
	var renewed types.LoginResponse
	require.NoError(t, json.Unmarshal(login(newPassword, http.StatusOK), &renewed))
	var profile types.User
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, member+"/info", renewed.Token, "", http.StatusOK), &profile))
	require.Equal(t, owner.ID, profile.Id)
	require.Len(t, profile.AuthMethods, 1)
	require.True(t, profile.AuthMethods[0].Verified)
}

func TestMemberContactChangesRotateSessionsOnlyAfterTheIdentityIsSaved(t *testing.T) {
	for _, method := range []string{"email", "mobile"} {
		t.Run(method, func(t *testing.T) {
			f := newRouteFixture(t)
			ctx := t.Context()
			f.service.Config.JwtAuth.AccessExpire = 300
			f.service.Config.Mobile.Enable = true
			owner, session := f.account(false)
			const password = "member-contact-fixture"
			f.client.User.UpdateOneID(owner.ID).SetPassword(tool.EncodePassWord(password)).SetAlgo(tool.PasswordAlgoArgon2id).ExecX(ctx)
			var readOnly bool
			f.client.UserAuthMethod.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
					if readOnly {
						return nil, errors.New("contact store read-only")
					}
					return next.Mutate(ctx, m)
				})
			})
			contacts := []string{"first@example.test", "updated@example.test"}
			if method == "mobile" {
				contacts = []string{"2025550123", "2025550124"}
			}
			for i, contact := range contacts {
				prefix, identifier := config.AuthCodeCacheKey, contact
				loginPath := "/v1/auth/login"
				fields := fmt.Sprintf(`"email":%q`, contact)
				loginFields := fields
				if method == "mobile" {
					prefix, identifier = config.AuthCodeTelephoneCacheKey, "+1"+contact
					loginPath += "/telephone"
					fields = fmt.Sprintf(`"area_code":"1","mobile":%q`, contact)
					loginFields = fmt.Sprintf(`"telephone_area_code":"1","telephone":%q`, contact)
				}
				bind := func(expected uint32) {
					t.Helper()
					key := fmt.Sprintf("%s:%s:%s", prefix, constant.Register, identifier)
					require.NoError(t, f.service.Redis.Set(ctx, key, `{"code":"412517"}`, time.Minute).Err())
					body := fmt.Sprintf(`{%s,"code":"412517","password":%q}`, fields, password)
					f.request(http.MethodPut, "/v1/public/user/bind_"+method, session, body, expected)
				}
				readOnly = true
				if i == 0 {
					bind(xerr.DatabaseInsertError)
					require.Zero(t, f.client.UserAuthMethod.Query().CountX(ctx))
				} else {
					before := f.client.UserAuthMethod.Query().OnlyX(ctx)
					bind(xerr.DatabaseUpdateError)
					require.Equal(t, before.AuthIdentifier, f.client.UserAuthMethod.GetX(ctx, before.ID).AuthIdentifier)
				}
				f.request(http.MethodGet, "/v1/public/user/info", session, "", http.StatusOK)
				readOnly = false
				bind(http.StatusOK)
				f.request(http.MethodGet, "/v1/public/user/info", session, "", xerr.InvalidAccess)
				identity := f.client.UserAuthMethod.Query().OnlyX(ctx)
				require.True(t, identity.Verified)
				require.Equal(t, identifier, identity.AuthIdentifier)
				var login types.LoginResponse
				require.NoError(t, json.Unmarshal(f.request(http.MethodPost, loginPath, "", fmt.Sprintf(`{%s,"password":%q}`, loginFields, password), http.StatusOK), &login))
				session = login.Token
				var member types.User
				require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/user/info", session, "", http.StatusOK), &member))
				require.Equal(t, owner.ID, member.Id)
			}
		})
	}
}

func TestReferredMemberCanSignInAfterOpeningWalletRecovers(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	referrer, _ := f.account(false)
	f.service.Config.JwtAuth.AccessExpire = 300
	f.service.Config.Mobile.Enable = true
	const referrerEmail = "sponsor@example.test"
	const email = "referred@example.test"
	const password = "referred-member-fixture"
	const path = "/v1/admin/user/"
	form := types.CreateUserRequest{Email: email, Password: password, TelephoneAreaCode: "1", Telephone: "2025550123", RefererUser: referrerEmail, Balance: 750, GiftAmount: 120, Commission: 15}
	create := func(want uint32) {
		t.Helper()
		body, err := json.Marshal(form)
		require.NoError(t, err)
		f.request(http.MethodPost, path, operator, string(body), want)
	}
	create(xerr.UserNotExist)
	f.client.UserAuthMethod.Create().SetUserID(referrer.ID).SetAuthType("email").SetAuthIdentifier(referrerEmail).SetVerified(true).SaveX(ctx)
	var interrupted string
	f.client.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if m.Op().Is(ent.OpCreate) && m.Type() == interrupted {
				return nil, errors.New("member provisioning interrupted")
			}
			return next.Mutate(ctx, m)
		})
	})
	for _, interrupted = range []string{ent.TypeUser, ent.TypeUserAuthMethod, ent.TypeUserWallet} {
		create(xerr.DatabaseInsertError)
		require.Equal(t, 2, f.client.User.Query().CountX(ctx))
		require.Equal(t, referrerEmail, f.client.UserAuthMethod.Query().OnlyX(ctx).AuthIdentifier)
		require.Zero(t, f.client.UserWallet.Query().CountX(ctx))
	}
	interrupted = ""
	create(http.StatusOK)
	create(xerr.TelephoneExist)
	wallet := f.client.UserWallet.Query().OnlyX(ctx)
	created, err := f.service.Store.User().FindOneByEmail(ctx, email)
	require.NoError(t, err)
	require.Equal(t, referrer.ID, created.RefererId)
	require.Equal(t, wallet.UserID, created.Id)
	for _, login := range []struct{ path, body string }{
		{"/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)},
		{"/v1/auth/login/telephone", fmt.Sprintf(`{"telephone_area_code":"1","telephone":"2025550123","password":%q}`, password)},
	} {
		var session types.LoginResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodPost, login.path, "", login.body, http.StatusOK), &session))
		var profile types.User
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/user/info", session.Token, "", http.StatusOK), &profile))
		require.Equal(t, created.Id, profile.Id)
		require.EqualValues(t, 750, profile.Balance)
		require.EqualValues(t, 120, profile.GiftAmount)
		require.EqualValues(t, 15, profile.Commission)
	}
}

func TestFirstWalletAdjustmentAndPasswordChangeCommitTogether(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	owner, originalSession := f.account(false)
	const email = "legacy.member@example.test"
	const oldPassword, newPassword = "legacy-member-password", "updated-member-password"
	f.service.Config.JwtAuth.AccessExpire = 300
	t.Setenv("PPANEL_MODE", "")
	originalHash := tool.EncodePassWord(oldPassword)
	f.client.User.UpdateOneID(owner.ID).SetPassword(originalHash).SetAlgo(tool.PasswordAlgoArgon2id).ExecX(ctx)
	f.client.UserAuthMethod.Create().SetUserID(owner.ID).SetAuthType("email").SetAuthIdentifier(email).SetVerified(true).SaveX(ctx)
	walletOffline := true
	f.client.UserWallet.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if walletOffline && mutation.Op().Is(ent.OpCreate) {
				return nil, errors.New("wallet creation unavailable")
			}
			return next.Mutate(ctx, mutation)
		})
	})
	body := fmt.Sprintf(`{"user_id":"%d","enable":true,"password":%q,"balance":1200,"gift_amount":180,"commission":35}`, owner.ID, newPassword)
	const edit, profilePath = "/v1/admin/user/basic", "/v1/public/user/info"
	f.request(http.MethodPut, edit, operator, body, xerr.DatabaseInsertError)
	require.Equal(t, originalHash, f.client.User.GetX(ctx, owner.ID).Password)
	require.Zero(t, f.client.UserWallet.Query().CountX(ctx))
	require.Zero(t, f.client.SystemLog.Query().CountX(ctx))
	var previous types.User
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, profilePath, originalSession, "", http.StatusOK), &previous))
	require.Equal(t, owner.ID, previous.Id)
	require.Zero(t, previous.Balance)
	walletOffline = false
	f.request(http.MethodPut, edit, operator, body, http.StatusOK)
	entries := f.client.SystemLog.Query().AllX(ctx)
	require.Len(t, entries, 3)
	changes := make(map[uint8]int64)
	for _, entry := range entries {
		require.Equal(t, owner.ID, entry.ObjectID)
		var change struct {
			Amount int64 `json:"amount"`
		}
		require.NoError(t, json.Unmarshal([]byte(entry.Content), &change))
		changes[entry.Type] = change.Amount
	}
	require.Equal(t, map[uint8]int64{log.TypeBalance.Uint8(): 1200, log.TypeGift.Uint8(): 180, log.TypeCommission.Uint8(): 35}, changes)
	f.request(http.MethodGet, profilePath, originalSession, "", xerr.InvalidAccess)
	login := func(password string, code uint32) json.RawMessage {
		t.Helper()
		return f.request(http.MethodPost, "/v1/auth/login", "", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password), code)
	}
	login(oldPassword, xerr.UserPasswordError)
	var renewed types.LoginResponse
	require.NoError(t, json.Unmarshal(login(newPassword, http.StatusOK), &renewed))
	var current types.User
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, profilePath, renewed.Token, "", http.StatusOK), &current))
	require.Equal(t, owner.ID, current.Id)
	require.Equal(t, [3]int64{1200, 180, 35}, [3]int64{current.Balance, current.GiftAmount, current.Commission})
}

func TestMemberProfileReflectsOperatorChangesAndArchival(t *testing.T) {
	f := newRouteFixture(t)
	_, operator := f.account(true)
	f.service.Config.JwtAuth.AccessExpire = 300
	const admin = "/v1/admin/user"
	const member = "/v1/public/user"
	const email = "billing.member@example.test"
	const credentials = `{"email":"billing.member@example.test","password":"member-fixture-password"}`
	const creation = `{"email":"billing.member@example.test","password":"member-fixture-password","balance":1250,"gift_amount":375,"commission":50}`
	f.request(http.MethodPost, admin+"/", operator, creation, http.StatusOK)
	f.request(http.MethodPost, admin+"/", operator, creation, xerr.EmailExist)
	list := func(archived bool) types.GetUserListResponse {
		t.Helper()
		var result types.GetUserListResponse
		path := fmt.Sprintf("%s/list?search=billing.member&page=1&size=10&unscoped=%t", admin, archived)
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path, operator, "", http.StatusOK), &result))
		return result
	}
	members := list(false)
	require.EqualValues(t, 1, members.Total)
	require.Len(t, members.List, 1)
	id := members.List[0].Id
	var session types.LoginResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/auth/login", "", credentials, http.StatusOK), &session))
	profile := func() types.User {
		t.Helper()
		var result types.User
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, member+"/info", session.Token, "", http.StatusOK), &result))
		return result
	}
	initial := profile()
	require.Equal(t, id, initial.Id)
	require.EqualValues(t, 1250, initial.Balance)
	require.EqualValues(t, 375, initial.GiftAmount)
	require.EqualValues(t, 50, initial.Commission)
	require.Len(t, initial.AuthMethods, 1)
	require.Equal(t, email, initial.AuthMethods[0].AuthIdentifier)

	binding := func(identifier string) string {
		return fmt.Sprintf(`{"user_id":"%d","auth_type":"github","auth_identifier":%q}`, id, identifier)
	}
	f.request(http.MethodPost, admin+"/auth_method", operator, binding("first-identity"), http.StatusOK)
	f.request(http.MethodPut, admin+"/auth_method", operator, binding("updated-identity"), http.StatusOK)
	var methods types.GetUserAuthMethodResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, fmt.Sprintf("%s/auth_method?user_id=%d", admin, id), operator, "", http.StatusOK), &methods))
	require.Contains(t, methods.AuthMethods, types.UserAuthMethod{AuthType: "github", AuthIdentifier: "updated-identity", Verified: true})
	withIdentity := profile()
	require.Len(t, withIdentity.AuthMethods, 2)
	require.Equal(t, "email", withIdentity.AuthMethods[0].AuthType)
	require.Equal(t, "github", withIdentity.AuthMethods[1].AuthType)
	require.Equal(t, "upd**********ity", withIdentity.AuthMethods[1].AuthIdentifier)
	f.request(http.MethodDelete, admin+"/auth_method", operator, binding("updated-identity"), http.StatusOK)
	require.Equal(t, initial.AuthMethods, profile().AuthMethods)

	f.request(http.MethodPut, admin+"/notify", operator, fmt.Sprintf(`{"user_id":"%d","enable_login_notify":true,"enable_subscribe_notify":true}`, id), http.StatusOK)
	prefs := profile()
	require.True(t, prefs.EnableLoginNotify)
	require.True(t, prefs.EnableSubscribeNotify)
	require.False(t, prefs.EnableBalanceNotify)
	f.request(http.MethodPut, member+"/notify", session.Token, `{"enable_login_notify":false,"enable_balance_notify":true}`, http.StatusOK)
	var detail types.User
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, fmt.Sprintf("%s/detail?id=%d", admin, id), operator, "", http.StatusOK), &detail))
	require.False(t, detail.EnableLoginNotify)
	require.True(t, detail.EnableBalanceNotify)
	require.True(t, detail.EnableSubscribeNotify)
	var logins types.GetUserLoginLogsResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, fmt.Sprintf("%s/login/logs?user_id=%d&page=1&size=5", admin, id), operator, "", http.StatusOK), &logins))
	require.Len(t, logins.List, 1)
	require.True(t, logins.List[0].Success)
	require.Equal(t, id, logins.List[0].UserId)

	f.request(http.MethodDelete, fmt.Sprintf("%s/?id=%d", admin, id), operator, "", http.StatusOK)
	require.Empty(t, list(false).List)
	history := list(true)
	require.Len(t, history.List, 1)
	require.Equal(t, id, history.List[0].Id)
	require.Equal(t, initial.Balance, history.List[0].Balance)
	f.request(http.MethodPost, "/v1/auth/login", "", credentials, xerr.UserNotExist)
}
