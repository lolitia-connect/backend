package oauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/internal/usersession"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

const telegramFixtureToken = "42:fixture-token"

func telegramLoginRequest(t *testing.T, client *ent.Client) *types.OAuthLoginGetTokenRequest {
	t.Helper()
	_, err := client.AuthMethod.Create().SetMethod(OAuthTelegram).SetEnabled(true).
		SetConfig(fmt.Sprintf(`{"bot_token":%q}`, telegramFixtureToken)).Save(t.Context())
	require.NoError(t, err)

	authDate := time.Now().Unix()
	secret := sha256.Sum256([]byte(telegramFixtureToken))
	signature := hmac.New(sha256.New, secret[:])
	fmt.Fprintf(signature, "auth_date=%d\nid=42", authDate)
	payload := fmt.Sprintf(`{"auth_date":%d,"id":42,"hash":"%x"}`, authDate, signature.Sum(nil))
	return &types.OAuthLoginGetTokenRequest{
		Method:   OAuthTelegram,
		Callback: map[string]any{"tgAuthResult": base64.RawURLEncoding.EncodeToString([]byte(payload))},
	}
}

func TestTelegramOAuthRegistrationAndSessionRevocation(t *testing.T) {
	service, client, cache, ctx := newOAuthTestService(t)
	login := NewOAuthLoginGetTokenLogic(ctx, service)
	register := NewOAuthRegisterLogic(ctx, service)
	request := telegramLoginRequest(t, client)
	const ip, agent, path = "192.0.2.10", "oauth-session-test", "/v1/user/profile"

	pending, err := login.OAuthLoginGetToken(request, ip, agent)
	require.NoError(t, err)
	require.NotNil(t, pending)
	require.True(t, pending.EmailRequired)
	require.NotEmpty(t, pending.OAuthToken)
	require.Empty(t, pending.Token)
	require.Zero(t, client.User.Query().CountX(ctx))
	sessions, err := cache.Keys(ctx, config.SessionIdKey+":*").Result()
	require.NoError(t, err)
	require.Empty(t, sessions)

	completion := &types.OAuthRegisterRequest{OAuthToken: pending.OAuthToken, Email: "oauth-session@example.com"}
	registered, err := register.OAuthRegister(completion, ip, agent)
	require.NoError(t, err)
	account := client.User.Query().OnlyX(ctx)
	registeredCtx, err := middleware.AuthenticateRequest(ctx, service, registered.Token, path)
	require.NoError(t, err)
	require.Equal(t, account.ID, registeredCtx.Value(constant.CtxKeyUser).(*user.User).Id)
	require.Equal(t, "oauth", registeredCtx.Value(constant.CtxLoginType))
	registeredSession := registeredCtx.Value(constant.CtxKeySessionID).(string)
	ttl, err := cache.TTL(ctx, config.SessionIdKey+":"+registeredSession).Result()
	require.NoError(t, err)
	require.InDelta(t, service.Config.JwtAuth.AccessExpire, ttl.Seconds(), 2)

	replayed, err := register.OAuthRegister(completion, ip, agent)
	requireOAuthErrorCode(t, err, xerr.InvalidAccess)
	require.Nil(t, replayed)

	_, err = usersession.Rotate(ctx, cache, account.ID)
	require.NoError(t, err)
	_, err = middleware.AuthenticateRequest(ctx, service, registered.Token, path)
	requireOAuthErrorCode(t, err, xerr.InvalidAccess)

	// The same widget result can be retried within its retry grace window.
	loggedIn, err := login.OAuthLoginGetToken(request, ip, agent)
	require.NoError(t, err)
	require.Empty(t, loggedIn.OAuthToken)
	loginCtx, err := middleware.AuthenticateRequest(ctx, service, loggedIn.Token, path)
	require.NoError(t, err)
	require.Equal(t, account.ID, loginCtx.Value(constant.CtxKeyUser).(*user.User).Id)
	require.NotEqual(t, registeredSession, loginCtx.Value(constant.CtxKeySessionID))
	require.Equal(t, 1, client.User.Query().CountX(ctx))
}

func TestTelegramOAuthDoesNotIssueSessionForDisabledAccount(t *testing.T) {
	service, client, cache, ctx := newOAuthTestService(t)
	request := telegramLoginRequest(t, client)
	account := client.User.Create().SetPassword("unused").SetEnable(false).SaveX(ctx)
	client.UserAuthMethod.Create().SetUserID(account.ID).SetAuthType(OAuthTelegram).
		SetAuthIdentifier("42").SetVerified(true).SaveX(ctx)

	response, err := NewOAuthLoginGetTokenLogic(ctx, service).OAuthLoginGetToken(request, "192.0.2.10", "oauth-session-test")
	requireOAuthErrorCode(t, err, xerr.UserDisabled)
	require.Nil(t, response)
	sessions, err := cache.Keys(ctx, config.SessionIdKey+":*").Result()
	require.NoError(t, err)
	require.Empty(t, sessions)
}

func requireOAuthErrorCode(t *testing.T, err error, want uint32) {
	t.Helper()
	var code *xerr.CodeError
	require.ErrorAs(t, err, &code)
	require.Equal(t, want, code.GetErrCode())
}
