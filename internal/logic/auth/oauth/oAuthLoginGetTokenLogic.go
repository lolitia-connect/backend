package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/perfect-panel/server/ent"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/auth"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/jwt"
	"github.com/perfect-panel/server/pkg/oauth/apple"
	"github.com/perfect-panel/server/pkg/oauth/google"
	"github.com/perfect-panel/server/pkg/oauth/telegram"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const (
	OAuthGoogle    = "google"
	OAuthApple     = "apple"
	OAuthTelegram  = "telegram"
	AuthEmail      = "email"
	AuthExpire     = 86400
	TelegramDomain = "ppanel.com"
)

type oauthRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}
type OAuthLoginGetTokenLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewOAuthLoginGetTokenLogic OAuth login get token
func NewOAuthLoginGetTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OAuthLoginGetTokenLogic {
	return &OAuthLoginGetTokenLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OAuthLoginGetTokenLogic) OAuthLoginGetToken(req *types.OAuthLoginGetTokenRequest, ip, userAgent string) (resp *types.LoginResponse, err error) {
	requestID := uuidx.NewUUID().String()
	loginStatus := false
	var userInfo *user.User

	l.Logger.Infow("oauth login request started",
		zap.Any("request_id", requestID),
		zap.Any("method", req.Method),
		zap.Any("ip", ip),
		zap.Any("user_agent", userAgent),
	)

	defer func() {
		l.recordLoginStatus(loginStatus, userInfo, ip, userAgent, requestID, req.Method)
	}()

	userInfo, err = l.handleOAuthProvider(req, requestID, ip, userAgent)
	if err != nil {
		return nil, err
	}

	token, err := l.generateToken(userInfo, requestID)
	if err != nil {
		return nil, err
	}

	loginStatus = true
	return &types.LoginResponse{Token: token}, nil
}

func (l *OAuthLoginGetTokenLogic) google(req *types.OAuthLoginGetTokenRequest, requestID, ip, userAgent string) (*user.User, error) {
	startTime := time.Now()
	l.Logger.Infow("google oauth processing started",
		zap.Any("request_id", requestID),
		zap.Any("provider", OAuthGoogle),
	)

	var request oauthRequest
	if err := tool.CloneMapToStruct(req.Callback.(map[string]interface{}), &request); err != nil {
		l.Logger.Errorw("failed to parse google callback data",
			zap.Any("request_id", requestID),
			zap.Any("provider", OAuthGoogle),
			zap.Any("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "parse callback data failed: %v", err)
	}

	l.Logger.Debugw("google oauth state validation started",
		zap.Any("request_id", requestID),
		zap.Any("state", request.State),
	)

	redirect, err := l.validateStateCode(OAuthGoogle, request.State, requestID)
	if err != nil {
		return nil, err
	}

	cfg, err := l.getGoogleConfig(requestID)
	if err != nil {
		return nil, err
	}

	client := google.New(&google.Config{
		ClientID:     cfg.ClientId,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  redirect,
	})

	l.Logger.Debugw("exchanging google authorization code for token",
		zap.Any("request_id", requestID),
		zap.Any("redirect_url", redirect),
	)

	token, err := client.Exchange(l.ctx, request.Code)
	if err != nil {
		l.Logger.Errorw("failed to exchange google authorization code",
			zap.Any("request_id", requestID),
			zap.Any("provider", OAuthGoogle),
			zap.Any("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "exchange token failed: %v", err)
	}

	l.Logger.Debugw("fetching google user information",
		zap.Any("request_id", requestID),
	)

	googleUserInfo, err := client.GetUserInfo(token.AccessToken)
	if err != nil {
		l.Logger.Errorw("failed to get google user info",
			zap.Any("request_id", requestID),
			zap.Any("provider", OAuthGoogle),
			zap.Any("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get user info failed: %v", err)
	}

	l.Logger.Infow("google oauth processing completed",
		zap.Any("request_id", requestID),
		zap.Any("provider", OAuthGoogle),
		zap.Any("openid", googleUserInfo.OpenID),
		zap.Any("email", googleUserInfo.Email),
		zap.Any("duration_ms", time.Since(startTime).Milliseconds()),
	)

	return l.findOrRegisterUser(OAuthGoogle, googleUserInfo.OpenID, googleUserInfo.Email, googleUserInfo.Picture, requestID, ip, userAgent)
}

func (l *OAuthLoginGetTokenLogic) apple(req *types.OAuthLoginGetTokenRequest, requestID, ip, userAgent string) (*user.User, error) {
	startTime := time.Now()
	l.Logger.Infow("apple oauth processing started",
		zap.Any("request_id", requestID),
		zap.Any("provider", OAuthApple),
	)

	callback := req.Callback.(map[string]interface{})
	state, _ := callback["state"].(string)
	code, _ := callback["code"].(string)

	l.Logger.Debugw("apple oauth state validation started",
		zap.Any("request_id", requestID),
		zap.Any("state", state),
	)

	if _, err := l.validateStateCode(OAuthApple, state, requestID); err != nil {
		return nil, err
	}

	cfg, err := l.getAppleConfig(requestID)
	if err != nil {
		return nil, err
	}

	client, err := apple.New(apple.Config{
		ClientID:     cfg.ClientId,
		TeamID:       cfg.TeamID,
		KeyID:        cfg.KeyID,
		ClientSecret: cfg.ClientSecret,
		RedirectURI:  cfg.RedirectURL,
	})
	if err != nil {
		l.Logger.Errorw("failed to create apple client",
			zap.Any("request_id", requestID),
			zap.Any("provider", OAuthApple),
			zap.Any("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "new apple client failed: %v", err)
	}

	l.Logger.Debugw("verifying apple web token",
		zap.Any("request_id", requestID),
	)

	resp, err := client.VerifyWebToken(l.ctx, code)
	if err != nil {
		l.Logger.Errorw("failed to verify apple web token",
			zap.Any("request_id", requestID),
			zap.Any("provider", OAuthApple),
			zap.Any("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "verify web token failed: %v", err)
	}

	if resp.Error != "" {
		l.Logger.Errorw("apple web token verification returned error",
			zap.Any("request_id", requestID),
			zap.Any("provider", OAuthApple),
			zap.Any("apple_error", resp.Error),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "verify web token failed: %v", resp.Error)
	}

	appleUnique, err := apple.GetUniqueID(resp.IDToken)
	if err != nil {
		l.Logger.Errorw("failed to get apple unique id",
			zap.Any("request_id", requestID),
			zap.Any("provider", OAuthApple),
			zap.Any("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get apple unique id failed: %v", err)
	}

	appleUserInfo, err := apple.GetClaims(resp.AccessToken)
	if err != nil {
		l.Logger.Errorw("failed to get apple user claims",
			zap.Any("request_id", requestID),
			zap.Any("provider", OAuthApple),
			zap.Any("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get apple user info failed: %v", err)
	}

	email := ""
	if emailVal, ok := (*appleUserInfo)["email"]; ok {
		email, _ = emailVal.(string)
	}

	l.Logger.Infow("apple oauth processing completed",
		zap.Any("request_id", requestID),
		zap.Any("provider", OAuthApple),
		zap.Any("unique_id", appleUnique),
		zap.Any("email", email),
		zap.Any("duration_ms", time.Since(startTime).Milliseconds()),
	)

	return l.findOrRegisterUser(OAuthApple, appleUnique, email, "", requestID, ip, userAgent)
}

func (l *OAuthLoginGetTokenLogic) telegram(req *types.OAuthLoginGetTokenRequest, requestID, ip, userAgent string) (*user.User, error) {
	startTime := time.Now()
	l.Logger.Infow("telegram oauth processing started",
		zap.Any("request_id", requestID),
		zap.Any("provider", OAuthTelegram),
	)

	cfg, err := l.getTelegramConfig(requestID)
	if err != nil {
		return nil, err
	}

	encodeText, _ := req.Callback.(map[string]interface{})["tgAuthResult"].(string)
	l.Logger.Debugw("parsing telegram callback data",
		zap.Any("request_id", requestID),
		zap.Any("data_length", len(encodeText)),
	)

	callbackData, err := telegram.ParseAndValidateBase64([]byte(encodeText), cfg.BotToken)
	if err != nil {
		l.Logger.Errorw("failed to parse telegram callback data",
			zap.Any("request_id", requestID),
			zap.Any("provider", OAuthTelegram),
			zap.Any("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "parse telegram callback failed: %v", err)
	}

	l.Logger.Debugw("validating telegram auth date",
		zap.Any("request_id", requestID),
		zap.Any("auth_date", *callbackData.AuthDate),
		zap.Any("current_time", time.Now().Unix()),
	)

	if time.Now().Unix()-*callbackData.AuthDate > AuthExpire {
		l.Logger.Errorw("telegram auth date expired",
			zap.Any("request_id", requestID),
			zap.Any("provider", OAuthTelegram),
			zap.Any("auth_date", *callbackData.AuthDate),
			zap.Any("current_time", time.Now().Unix()),
			zap.Any("expire_seconds", AuthExpire),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "auth date expired")
	}

	userID := fmt.Sprintf("%v", *callbackData.Id)
	email := fmt.Sprintf("%v@%s", *callbackData.Id, TelegramDomain)
	avatar := ""
	if callbackData.PhotoUrl != nil {
		avatar = *callbackData.PhotoUrl
	}

	l.Logger.Infow("telegram oauth processing completed",
		zap.Any("request_id", requestID),
		zap.Any("provider", OAuthTelegram),
		zap.Any("user_id", userID),
		zap.Any("email", email),
		zap.Any("duration_ms", time.Since(startTime).Milliseconds()),
	)

	return l.findOrRegisterUser(OAuthTelegram, userID, email, avatar, requestID, ip, userAgent)
}

func (l *OAuthLoginGetTokenLogic) register(email, avatar, method, openid, requestID, ip, userAgent string) (*user.User, error) {
	startTime := time.Now()
	l.Logger.Infow("user registration started",
		zap.Any("request_id", requestID),
		zap.Any("auth_method", method),
		zap.Any("email", email),
		zap.Any("openid", openid),
	)

	if l.svcCtx.Config.Invite.ForcedInvite {
		l.Logger.Errorw("registration blocked due to forced invite policy",
			zap.Any("request_id", requestID),
			zap.Any("auth_method", method),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InviteCodeError), "invite code is required")
	}

	var userInfo *user.User
	var trialSubscribe *user.Subscribe
	err := l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		if email != "" {
			l.Logger.Debugw("checking if email already exists",
				zap.Any("request_id", requestID),
				zap.Any("email", email),
			)
			if err := l.checkEmailExists(store, email, requestID); err != nil {
				return err
			}
		}

		l.Logger.Debugw("creating new user record",
			zap.Any("request_id", requestID),
			zap.Any("avatar", avatar),
		)

		userInfo = &user.User{Avatar: avatar, OnlyFirstPurchase: &l.svcCtx.Config.Invite.OnlyFirstPurchase}
		if err := store.User().Insert(l.ctx, userInfo); err != nil {
			l.Logger.Errorw("failed to create user record",
				zap.Any("request_id", requestID),
				zap.Any("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create user info failed: %v", err)
		}

		userInfo.ReferCode = uuidx.UserInviteCode(userInfo.Id)
		l.Logger.Debugw("updating user refer code",
			zap.Any("request_id", requestID),
			zap.Any("user_id", userInfo.Id),
			zap.Any("refer_code", userInfo.ReferCode),
		)

		if err := store.User().Update(l.ctx, userInfo); err != nil {
			l.Logger.Errorw("failed to update refer code",
				zap.Any("request_id", requestID),
				zap.Any("user_id", userInfo.Id),
				zap.Any("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update refer code failed: %v", err)
		}

		if err := l.createAuthMethod(store, userInfo.Id, method, openid, requestID); err != nil {
			return err
		}

		if email != "" {
			if err := l.createAuthMethod(store, userInfo.Id, AuthEmail, email, requestID); err != nil {
				return err
			}
		}

		if l.svcCtx.Config.Register.EnableTrial {
			l.Logger.Debugw("activating trial subscription",
				zap.Any("request_id", requestID),
				zap.Any("user_id", userInfo.Id),
			)
			var trialErr error
			trialSubscribe, trialErr = l.activeTrial(store, userInfo.Id, requestID)
			if trialErr != nil {
				return trialErr
			}
		}

		return nil
	})

	if err != nil {
		l.Logger.Errorw("user registration failed",
			zap.Any("request_id", requestID),
			zap.Any("auth_method", method),
			zap.Any("error", err.Error()),
			zap.Any("duration_ms", time.Since(startTime).Milliseconds()),
		)
		return userInfo, err
	}
	clearTrialSubscribeCache(l.ctx, l.svcCtx, trialSubscribe)

	// Clear cache after transaction success
	if l.svcCtx.Config.Register.EnableTrial && trialSubscribe != nil {
		// Clear subscription cache
		if err = l.svcCtx.Store.Subscribe().ClearCache(l.ctx, trialSubscribe.SubscribeId); err != nil {
			l.Logger.Errorw("ClearSubscribeCache failed", zap.Any("error", err.Error()), zap.Any("subscribeId", trialSubscribe.SubscribeId))
			// Don't return error, just log it
		}
		// Clear all server cache
		if err = l.svcCtx.Store.Node().ClearServerAllCache(l.ctx); err != nil {
			l.Logger.Errorf("ClearServerAllCache error: %v", err.Error())
			// Don't return error, just log it
		}
	}

	l.Logger.Infow("user registration completed successfully",
		zap.Any("request_id", requestID),
		zap.Any("user_id", userInfo.Id),
		zap.Any("auth_method", method),
		zap.Any("email", email),
		zap.Any("refer_code", userInfo.ReferCode),
		zap.Any("duration_ms", time.Since(startTime).Milliseconds()),
	)

	// Register log
	registerLog := log.Register{
		AuthMethod: method,
		Identifier: openid,
		RegisterIP: ip,
		UserAgent:  userAgent,
		Timestamp:  time.Now().UnixMilli(),
	}
	content, _ := registerLog.Marshal()

	err = l.svcCtx.Store.Log().Insert(l.ctx, &log.SystemLog{
		Type:     log.TypeRegister.Uint8(),
		Date:     time.Now().Format("2006-01-02"),
		ObjectID: userInfo.Id,
		Content:  string(content),
	})
	if err != nil {
		l.Logger.Errorw("failed to insert register log",
			zap.Any("request_id", requestID),
			zap.Any("user_id", userInfo.Id),
			zap.Any("ip", ip),
			zap.Any("error", err.Error()),
		)
	}

	return userInfo, err
}

func (l *OAuthLoginGetTokenLogic) checkEmailExists(store repository.Store, email, requestID string) error {
	userInfo, err := store.User().FindOneByEmail(l.ctx, email)
	if err != nil && !ent.IsNotFound(err) {
		l.Logger.Errorw("failed to check email existence",
			zap.Any("request_id", requestID),
			zap.Any("email", email),
			zap.Any("error", err.Error()),
		)
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "check email exists failed: %v", err)
	}
	if err == nil && userInfo != nil && userInfo.Id != 0 {
		l.Logger.Errorw("email already exists for another user",
			zap.Any("request_id", requestID),
			zap.Any("email", email),
			zap.Any("existing_user_id", userInfo.Id),
		)
		return errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "user email exist: %v", email)
	}
	l.Logger.Debugw("email availability confirmed",
		zap.Any("request_id", requestID),
		zap.Any("email", email),
	)
	return nil
}

func (l *OAuthLoginGetTokenLogic) createAuthMethod(store repository.Store, userID int64, authType, identifier, requestID string) error {
	l.Logger.Debugw("creating auth method",
		zap.Any("request_id", requestID),
		zap.Any("user_id", userID),
		zap.Any("auth_type", authType),
		zap.Any("identifier", identifier),
	)

	authMethod := &user.AuthMethods{
		UserId:         userID,
		AuthType:       authType,
		AuthIdentifier: identifier,
		Verified:       true,
	}
	if err := store.User().InsertUserAuthMethods(l.ctx, authMethod); err != nil {
		l.Logger.Errorw("failed to create auth method",
			zap.Any("request_id", requestID),
			zap.Any("user_id", userID),
			zap.Any("auth_type", authType),
			zap.Any("identifier", identifier),
			zap.Any("error", err.Error()),
		)
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create auth method failed: %v", err)
	}

	l.Logger.Debugw("auth method created successfully",
		zap.Any("request_id", requestID),
		zap.Any("user_id", userID),
		zap.Any("auth_type", authType),
		zap.Any("auth_method_id", authMethod.Id),
	)
	return nil
}

func (l *OAuthLoginGetTokenLogic) recordLoginStatus(loginStatus bool, userInfo *user.User, ip, userAgent, requestID, authType string) {

	if userInfo != nil && userInfo.Id != 0 {
		loginLog := log.Login{
			Method:    authType,
			LoginIP:   ip,
			UserAgent: userAgent,
			Success:   loginStatus,
			Timestamp: time.Now().UnixMilli(),
		}
		content, _ := loginLog.Marshal()
		if err := l.svcCtx.Store.Log().Insert(l.ctx, &log.SystemLog{
			Type:     log.TypeLogin.Uint8(),
			Date:     time.Now().Format("2006-01-02"),
			ObjectID: userInfo.Id,
			Content:  string(content),
		}); err != nil {
			l.Logger.Errorw("failed to insert login log",
				zap.Any("request_id", requestID),
				zap.Any("user_id", userInfo.Id),
				zap.Any("ip", ip),
				zap.Any("error", err.Error()),
			)
		}
	}
}

func (l *OAuthLoginGetTokenLogic) handleOAuthProvider(req *types.OAuthLoginGetTokenRequest, requestID, ip, userAgent string) (*user.User, error) {
	l.Logger.Debugw("handling oauth provider",
		zap.Any("request_id", requestID),
		zap.Any("provider", req.Method),
	)

	switch req.Method {
	case OAuthGoogle:
		return l.google(req, requestID, ip, userAgent)
	case OAuthApple:
		return l.apple(req, requestID, ip, userAgent)
	case OAuthTelegram:
		return l.telegram(req, requestID, ip, userAgent)
	default:
		l.Logger.Errorw("unsupported oauth login method",
			zap.Any("request_id", requestID),
			zap.Any("method", req.Method),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "oauth login method not supported: %v", req.Method)
	}
}

func (l *OAuthLoginGetTokenLogic) generateToken(userInfo *user.User, requestID string) (string, error) {
	startTime := time.Now()
	sessionId := uuidx.NewUUID().String()

	l.Logger.Debugw("generating jwt token",
		zap.Any("request_id", requestID),
		zap.Any("user_id", userInfo.Id),
		zap.Any("session_id", sessionId),
	)

	token, err := jwt.NewJwtToken(
		l.svcCtx.Config.JwtAuth.AccessSecret,
		time.Now().Unix(),
		l.svcCtx.Config.JwtAuth.AccessExpire,
		jwt.WithOption("UserId", userInfo.Id),
		jwt.WithOption("SessionId", sessionId),
	)
	if err != nil {
		l.Logger.Errorw("failed to generate jwt token",
			zap.Any("request_id", requestID),
			zap.Any("user_id", userInfo.Id),
			zap.Any("error", err.Error()),
		)
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "token generate error: %v", err)
	}

	sessionIdCacheKey := fmt.Sprintf("%v:%v", config.SessionIdKey, sessionId)
	if err = l.svcCtx.Redis.Set(l.ctx, sessionIdCacheKey, userInfo.Id, time.Duration(l.svcCtx.Config.JwtAuth.AccessExpire)*time.Second).Err(); err != nil {
		l.Logger.Errorw("failed to cache session id",
			zap.Any("request_id", requestID),
			zap.Any("user_id", userInfo.Id),
			zap.Any("session_id", sessionId),
			zap.Any("error", err.Error()),
		)
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "set session id error: %v", err)
	}

	l.Logger.Infow("jwt token generated successfully",
		zap.Any("request_id", requestID),
		zap.Any("user_id", userInfo.Id),
		zap.Any("session_id", sessionId),
		zap.Any("duration_ms", time.Since(startTime).Milliseconds()),
	)

	return token, nil
}

func (l *OAuthLoginGetTokenLogic) validateStateCode(provider, state, requestID string) (string, error) {
	stateKey := fmt.Sprintf("%s:%s", provider, state)
	l.Logger.Debugw("validating oauth state code",
		zap.Any("request_id", requestID),
		zap.Any("provider", provider),
		zap.Any("state_key", stateKey),
	)

	redirect, err := l.svcCtx.Redis.Get(l.ctx, stateKey).Result()
	if err != nil {
		l.Logger.Errorw("failed to validate state code",
			zap.Any("request_id", requestID),
			zap.Any("provider", provider),
			zap.Any("state_key", stateKey),
			zap.Any("error", err.Error()),
		)
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get %s state code failed: %v", provider, err)
	}

	l.Logger.Debugw("state code validated successfully",
		zap.Any("request_id", requestID),
		zap.Any("provider", provider),
		zap.Any("redirect_url", redirect),
	)
	return redirect, nil
}

func (l *OAuthLoginGetTokenLogic) getGoogleConfig(requestID string) (*auth.GoogleAuthConfig, error) {
	l.Logger.Debugw("fetching google oauth config",
		zap.Any("request_id", requestID),
		zap.Any("provider", OAuthGoogle),
	)

	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, OAuthGoogle)
	if err != nil {
		l.Logger.Errorw("failed to find google auth method",
			zap.Any("request_id", requestID),
			zap.Any("provider", OAuthGoogle),
			zap.Any("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find google auth method failed: %v", err)
	}

	var cfg auth.GoogleAuthConfig
	if err = cfg.Unmarshal(authMethod.Config); err != nil {
		l.Logger.Errorw("failed to unmarshal google config",
			zap.Any("request_id", requestID),
			zap.Any("provider", OAuthGoogle),
			zap.Any("config", authMethod.Config),
			zap.Any("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal google config failed: %v", err)
	}

	l.Logger.Debugw("google oauth config loaded successfully",
		zap.Any("request_id", requestID),
		zap.Any("provider", OAuthGoogle),
		zap.Any("client_id", cfg.ClientId),
	)
	return &cfg, nil
}

func (l *OAuthLoginGetTokenLogic) getAppleConfig(requestID string) (*auth.AppleAuthConfig, error) {
	l.Logger.Debugw("fetching apple oauth config",
		zap.Any("request_id", requestID),
		zap.Any("provider", OAuthApple),
	)

	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, OAuthApple)
	if err != nil {
		l.Logger.Errorw("failed to find apple auth method",
			zap.Any("request_id", requestID),
			zap.Any("provider", OAuthApple),
			zap.Any("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find apple auth method failed: %v", err)
	}

	var cfg auth.AppleAuthConfig
	if err = cfg.Unmarshal(authMethod.Config); err != nil {
		l.Logger.Errorw("failed to unmarshal apple config",
			zap.Any("request_id", requestID),
			zap.Any("provider", OAuthApple),
			zap.Any("config", authMethod.Config),
			zap.Any("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal apple config failed: %v", err)
	}

	l.Logger.Debugw("apple oauth config loaded successfully",
		zap.Any("request_id", requestID),
		zap.Any("provider", OAuthApple),
		zap.Any("client_id", cfg.ClientId),
		zap.Any("team_id", cfg.TeamID),
	)
	return &cfg, nil
}

func (l *OAuthLoginGetTokenLogic) getTelegramConfig(requestID string) (*auth.TelegramAuthConfig, error) {
	l.Logger.Debugw("fetching telegram oauth config",
		zap.Any("request_id", requestID),
		zap.Any("provider", OAuthTelegram),
	)

	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, OAuthTelegram)
	if err != nil {
		l.Logger.Errorw("failed to find telegram auth method",
			zap.Any("request_id", requestID),
			zap.Any("provider", OAuthTelegram),
			zap.Any("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find telegram auth method failed: %v", err)
	}

	var cfg auth.TelegramAuthConfig
	if err = json.Unmarshal([]byte(authMethod.Config), &cfg); err != nil {
		l.Logger.Errorw("failed to unmarshal telegram config",
			zap.Any("request_id", requestID),
			zap.Any("provider", OAuthTelegram),
			zap.Any("config", authMethod.Config),
			zap.Any("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal telegram config failed: %v", err)
	}

	l.Logger.Debugw("telegram oauth config loaded successfully",
		zap.Any("request_id", requestID),
		zap.Any("provider", OAuthTelegram),
	)
	return &cfg, nil
}

func (l *OAuthLoginGetTokenLogic) findOrRegisterUser(authType, openID, email, avatar, requestID, ip, userAgent string) (*user.User, error) {
	l.Logger.Debugw("finding or registering user",
		zap.Any("request_id", requestID),
		zap.Any("auth_type", authType),
		zap.Any("openid", openID),
		zap.Any("email", email),
	)

	userAuthMethod, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, authType, openID)
	if err != nil {
		if ent.IsNotFound(err) {
			l.Logger.Infow("user not found, starting registration",
				zap.Any("request_id", requestID),
				zap.Any("auth_type", authType),
				zap.Any("openid", openID),
				zap.Any("email", email),
			)
			return l.register(email, avatar, authType, openID, requestID, ip, userAgent)
		}
		l.Logger.Errorw("failed to find user auth method by openid",
			zap.Any("request_id", requestID),
			zap.Any("auth_type", authType),
			zap.Any("openid", openID),
			zap.Any("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user auth method by openid failed: %v", err)
	}

	l.Logger.Debugw("found existing user auth method",
		zap.Any("request_id", requestID),
		zap.Any("auth_type", authType),
		zap.Any("user_id", userAuthMethod.UserId),
	)

	userInfo, err := l.svcCtx.Store.User().FindOne(l.ctx, userAuthMethod.UserId)
	if err != nil {
		l.Logger.Errorw("failed to find user by id",
			zap.Any("request_id", requestID),
			zap.Any("user_id", userAuthMethod.UserId),
			zap.Any("error", err.Error()),
		)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user info failed: %v", err)
	}

	l.Logger.Infow("existing user found successfully",
		zap.Any("request_id", requestID),
		zap.Any("user_id", userInfo.Id),
		zap.Any("auth_type", authType),
	)

	return userInfo, nil
}

func (l *OAuthLoginGetTokenLogic) activeTrial(store repository.Store, uid int64, requestID string) (*user.Subscribe, error) {
	l.Logger.Debugw("fetching trial subscription template",
		zap.Any("request_id", requestID),
		zap.Any("user_id", uid),
		zap.Any("trial_subscribe_id", l.svcCtx.Config.Register.TrialSubscribe),
	)

	sub, err := store.Subscribe().FindOne(l.ctx, l.svcCtx.Config.Register.TrialSubscribe)
	if err != nil {
		l.Logger.Errorw("failed to find trial subscription template",
			zap.Any("request_id", requestID),
			zap.Any("user_id", uid),
			zap.Any("trial_subscribe_id", l.svcCtx.Config.Register.TrialSubscribe),
			zap.Any("error", err.Error()),
		)
		return nil, err
	}

	startTime := time.Now()
	expireTime := tool.AddTime(l.svcCtx.Config.Register.TrialTimeUnit, l.svcCtx.Config.Register.TrialTime, startTime)
	subscribeToken := uuidx.SubscribeToken(fmt.Sprintf("Trial-%v-%s", uid, uuidx.NewUUID().String()))
	subscribeUUID := uuidx.NewUUID().String()

	l.Logger.Debugw("creating trial subscription",
		zap.Any("request_id", requestID),
		zap.Any("user_id", uid),
		zap.Any("subscribe_id", sub.Id),
		zap.Any("start_time", startTime),
		zap.Any("expire_time", expireTime),
		zap.Any("traffic", sub.Traffic),
		zap.Any("token", subscribeToken),
		zap.Any("uuid", subscribeUUID),
	)

	userSub := &user.Subscribe{
		Id:          0,
		UserId:      uid,
		OrderId:     0,
		SubscribeId: sub.Id,
		StartTime:   startTime,
		ExpireTime:  expireTime,
		Traffic:     sub.Traffic,
		Download:    0,
		Upload:      0,
		Token:       subscribeToken,
		UUID:        subscribeUUID,
		Status:      1,
	}

	if err := store.User().InsertSubscribe(l.ctx, userSub); err != nil {
		l.Logger.Errorw("failed to insert trial subscription",
			zap.Any("request_id", requestID),
			zap.Any("user_id", uid),
			zap.Any("error", err.Error()),
		)
		return nil, err
	}

	l.Logger.Infow("trial subscription activated successfully",
		zap.Any("request_id", requestID),
		zap.Any("user_id", uid),
		zap.Any("subscribe_id", sub.Id),
		zap.Any("expire_time", expireTime),
		zap.Any("traffic", sub.Traffic),
	)
	return userSub, nil
}
