package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

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
