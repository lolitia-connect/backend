package user

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/perfect-panel/server/ent"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/auth"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/oauth/apple"
	"github.com/perfect-panel/server/pkg/oauth/facebook"
	"github.com/perfect-panel/server/pkg/oauth/github"
	"github.com/perfect-panel/server/pkg/oauth/google"
	"github.com/perfect-panel/server/pkg/oauth/telegram"
	"github.com/perfect-panel/server/pkg/oauthstate"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// telegramBindAuthExpire bounds how stale a Telegram widget result may be
// when binding, and telegramBindRetryGrace lets a client re-submit the same
// result after a timed-out request without treating it as a replay.
const (
	telegramBindAuthExpire = 300
	telegramBindRetryGrace = 60 * time.Second
)

type BindOAuthCallbackLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Bind OAuth Callback
func NewBindOAuthCallbackLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BindOAuthCallbackLogic {
	return &BindOAuthCallbackLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

type googleRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

func (l *BindOAuthCallbackLogic) BindOAuthCallback(req *types.BindOAuthCallbackRequest) error {
	_, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	var err error
	switch req.Method {
	case "google":
		err = l.google(req)
	case "apple":
		err = l.apple(req)
	case "telegram":
		err = l.telegram(req)
	case "github":
		err = l.github(req)
	case "facebook":
		err = l.facebook(req)
	default:
		l.Logger.Errorw("oauth login method not support", zap.Any("method", req.Method))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "oauth login method not support: %v", req.Method)
	}
	if err != nil {
		l.Logger.Errorw("bind oauth callback failed: %v", zap.Any("error", err.Error()))
		return err
	}
	return nil
}
func (l *BindOAuthCallbackLogic) google(req *types.BindOAuthCallbackRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	var request googleRequest
	err := tool.CloneMapToStruct(req.Callback.(map[string]interface{}), &request)
	if err != nil {
		l.Logger.Errorw("error CloneMapToStruct: %v", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "CloneMapToStruct failed")
	}
	// validate the state code
	redirect, err := l.svcCtx.Redis.Get(l.ctx, fmt.Sprintf("google:%s", request.State)).Result()
	if err != nil {
		l.Logger.Errorw("error get google state code: %v", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get google state code failed")
	}
	// get google config
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "google")
	if err != nil {
		l.Logger.Errorw("error find google auth method: %v", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find google auth method failed")
	}
	var cfg auth.GoogleAuthConfig
	err = json.Unmarshal([]byte(authMethod.Config), &cfg)
	if err != nil {
		l.Logger.Errorw("error unmarshal google config: %v", zap.Any("config", authMethod.Config), zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal google config failed")
	}
	client := google.New(&google.Config{
		ClientID:     cfg.ClientId,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  redirect,
	})
	token, err := client.Exchange(l.ctx, request.Code)
	if err != nil {
		l.Logger.Errorw("error exchange google token: %v", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "exchange google token failed")
	}
	googleUserInfo, err := client.GetUserInfo(token.AccessToken)
	if err != nil {
		l.Logger.Errorw("error get google user info: %v", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get google user info failed")
	}
	// query user info
	userAuthMethod, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, "google", googleUserInfo.OpenID)
	if err != nil && !ent.IsNotFound(err) {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query user auth method failed")
	}
	if userAuthMethod != nil && userAuthMethod.Id > 0 {
		return errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "google user already exists")
	}
	// bind google
	userAuthMethod = &user.AuthMethods{
		UserId:         u.Id,
		AuthType:       "google",
		AuthIdentifier: googleUserInfo.OpenID,
		Verified:       true,
	}
	err = l.svcCtx.Store.User().InsertUserAuthMethods(l.ctx, userAuthMethod)
	if err != nil {
		l.Logger.Errorw("error insert user auth method: %v", zap.Any("error", err.Error()))
		return err
	}
	return nil
}

func (l *BindOAuthCallbackLogic) github(req *types.BindOAuthCallbackRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	var request googleRequest
	if err := tool.CloneMapToStruct(req.Callback.(map[string]interface{}), &request); err != nil {
		l.Logger.Errorw("error CloneMapToStruct: %v", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "CloneMapToStruct failed")
	}
	redirect, err := l.svcCtx.Redis.Get(l.ctx, fmt.Sprintf("github:%s", request.State)).Result()
	if err != nil {
		l.Logger.Errorw("error get github state code: %v", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get github state code failed")
	}
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "github")
	if err != nil {
		l.Logger.Errorw("error find github auth method: %v", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find github auth method failed")
	}
	var cfg auth.GithubAuthConfig
	if err = json.Unmarshal([]byte(authMethod.Config), &cfg); err != nil {
		l.Logger.Errorw("error unmarshal github config: %v", zap.Any("config", authMethod.Config), zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal github config failed")
	}
	client := github.New(&github.Config{
		ClientID:     cfg.ClientId,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  redirect,
	})
	token, err := client.Exchange(l.ctx, request.Code)
	if err != nil {
		l.Logger.Errorw("error exchange github token: %v", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "exchange github token failed")
	}
	githubUserInfo, err := client.GetUserInfo(token.AccessToken)
	if err != nil {
		l.Logger.Errorw("error get github user info: %v", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get github user info failed")
	}
	openID := strconv.FormatInt(githubUserInfo.OpenID, 10)
	userAuthMethod, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, "github", openID)
	if err != nil && !ent.IsNotFound(err) {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query user auth method failed")
	}
	if userAuthMethod != nil && userAuthMethod.Id > 0 {
		return errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "github user already exists")
	}
	userAuthMethod = &user.AuthMethods{
		UserId:         u.Id,
		AuthType:       "github",
		AuthIdentifier: openID,
		Verified:       true,
	}
	if err = l.svcCtx.Store.User().InsertUserAuthMethods(l.ctx, userAuthMethod); err != nil {
		l.Logger.Errorw("error insert user auth method: %v", zap.Any("error", err.Error()))
		return err
	}
	return nil
}

func (l *BindOAuthCallbackLogic) facebook(req *types.BindOAuthCallbackRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	var request googleRequest
	if err := tool.CloneMapToStruct(req.Callback.(map[string]interface{}), &request); err != nil {
		l.Logger.Errorw("error CloneMapToStruct: %v", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "CloneMapToStruct failed")
	}
	redirect, err := l.svcCtx.Redis.Get(l.ctx, fmt.Sprintf("facebook:%s", request.State)).Result()
	if err != nil {
		l.Logger.Errorw("error get facebook state code: %v", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get facebook state code failed")
	}
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "facebook")
	if err != nil {
		l.Logger.Errorw("error find facebook auth method: %v", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find facebook auth method failed")
	}
	var cfg auth.FacebookAuthConfig
	if err = json.Unmarshal([]byte(authMethod.Config), &cfg); err != nil {
		l.Logger.Errorw("error unmarshal facebook config: %v", zap.Any("config", authMethod.Config), zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal facebook config failed")
	}
	client := facebook.New(&facebook.Config{
		ClientID:     cfg.ClientId,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  redirect,
	})
	token, err := client.Exchange(l.ctx, request.Code)
	if err != nil {
		l.Logger.Errorw("error exchange facebook token: %v", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "exchange facebook token failed")
	}
	facebookUserInfo, err := client.GetUserInfo(token.AccessToken)
	if err != nil {
		l.Logger.Errorw("error get facebook user info: %v", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get facebook user info failed")
	}
	userAuthMethod, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, "facebook", facebookUserInfo.OpenID)
	if err != nil && !ent.IsNotFound(err) {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query user auth method failed")
	}
	if userAuthMethod != nil && userAuthMethod.Id > 0 {
		return errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "facebook user already exists")
	}
	userAuthMethod = &user.AuthMethods{
		UserId:         u.Id,
		AuthType:       "facebook",
		AuthIdentifier: facebookUserInfo.OpenID,
		Verified:       true,
	}
	if err = l.svcCtx.Store.User().InsertUserAuthMethods(l.ctx, userAuthMethod); err != nil {
		l.Logger.Errorw("error insert user auth method: %v", zap.Any("error", err.Error()))
		return err
	}
	return nil
}

func (l *BindOAuthCallbackLogic) apple(req *types.BindOAuthCallbackRequest) error {
	// validate the state code
	_, err := l.svcCtx.Redis.Get(l.ctx, fmt.Sprintf("apple:%s", req.Callback.(map[string]interface{})["state"])).Result()
	if err != nil {
		l.Logger.Errorw("[BindOAuthCallbackLogic] Get State code error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get apple state code failed: %v", err.Error())
	}
	appleAuth, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "apple")
	if err != nil {
		l.Logger.Errorw("[BindOAuthCallbackLogic] FindOneByMethod error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find apple auth method failed: %v", err.Error())
	}
	var appleCfg auth.AppleAuthConfig
	err = json.Unmarshal([]byte(appleAuth.Config), &appleCfg)
	if err != nil {
		l.Logger.Errorw("[BindOAuthCallbackLogic] Unmarshal error", zap.Any("error", err.Error()), zap.Any("config", appleAuth.Config))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal apple config failed: %v", err.Error())
	}

	client, err := apple.New(apple.Config{
		ClientID:     appleCfg.ClientId,
		TeamID:       appleCfg.TeamID,
		KeyID:        appleCfg.KeyID,
		ClientSecret: appleCfg.ClientSecret,
		RedirectURI:  appleCfg.RedirectURL,
	})
	if err != nil {
		l.Logger.Errorw("[BindOAuthCallbackLogic] New apple client error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "new apple client failed: %v", err.Error())
	}
	// verify web token
	resp, err := client.VerifyWebToken(l.ctx, req.Callback.(map[string]interface{})["code"].(string))
	if err != nil {
		l.Logger.Errorw("[BindOAuthCallbackLogic] VerifyWebToken error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "verify web token failed: %v", err.Error())
	}
	if resp.Error != "" {
		l.Logger.Errorw("[BindOAuthCallbackLogic] VerifyWebToken error", zap.Any("error", resp.Error))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "verify web token failed: %v", resp.Error)
	}
	// query apple user unique id
	appleUnique, err := apple.GetUniqueID(resp.IDToken)
	if err != nil {
		l.Logger.Errorw("[BindOAuthCallbackLogic] GetUniqueID error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get apple unique id failed: %v", err.Error())
	}
	// query user by apple unique id
	userAuthMethod, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, "apple", appleUnique)
	if err != nil && !ent.IsNotFound(err) {
		l.Logger.Errorw("[BindOAuthCallbackLogic] FindUserAuthMethodByOpenID error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user auth method by openid failed: %v", err.Error())
	}
	if userAuthMethod != nil && userAuthMethod.Id > 0 {
		l.Logger.Errorw("[BindOAuthCallbackLogic] User already exists")
		return errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "apple user already exists")
	}
	// query user info
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	// bind apple
	userAuthMethod = &user.AuthMethods{
		UserId:         u.Id,
		AuthType:       "apple",
		AuthIdentifier: appleUnique,
		Verified:       true,
	}
	err = l.svcCtx.Store.User().InsertUserAuthMethods(l.ctx, userAuthMethod)
	if err != nil {
		l.Logger.Errorw("[BindOAuthCallbackLogic] InsertUserAuthMethods error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert user auth method failed: %v", err.Error())
	}
	return nil
}

func (l *BindOAuthCallbackLogic) telegram(req *types.BindOAuthCallbackRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	callback, ok := req.Callback.(map[string]interface{})
	if !ok {
		l.Logger.Errorw("invalid telegram callback payload", zap.Any("callback_type", fmt.Sprintf("%T", req.Callback)))
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "invalid telegram callback payload")
	}

	encodeText, ok := callback["tgAuthResult"].(string)
	if !ok || encodeText == "" {
		l.Logger.Errorw("telegram callback payload missing tgAuthResult")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "invalid telegram callback payload")
	}

	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "telegram")
	if err != nil {
		l.Logger.Errorw("find telegram auth method failed", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find telegram auth method failed")
	}

	var cfg auth.TelegramAuthConfig
	err = cfg.Unmarshal(authMethod.Config)
	if err != nil {
		l.Logger.Errorw("unmarshal telegram config failed", zap.Any("config", authMethod.Config), zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal telegram config failed")
	}

	callbackData, err := telegram.ParseAndValidateBase64([]byte(encodeText), cfg.BotToken)
	if err != nil {
		l.Logger.Errorw("parse telegram callback failed", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "parse telegram callback failed")
	}

	if callbackData.Id == nil || callbackData.AuthDate == nil {
		l.Logger.Errorw("telegram callback payload missing required fields")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "invalid telegram callback payload")
	}

	if time.Now().Unix()-*callbackData.AuthDate > telegramBindAuthExpire {
		l.Logger.Errorw("telegram auth date expired",
			zap.Any("auth_date", *callbackData.AuthDate),
			zap.Any("current_time", time.Now().Unix()),
			zap.Any("expire_seconds", telegramBindAuthExpire),
		)
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "auth date expired")
	}

	// The signature alone does not bind the result to one exchange, so the
	// payload is redeemable once (plus a short retry grace). A Redis outage
	// must not lock users out, so it degrades to signature and freshness.
	claimKey := fmt.Sprintf("%s:%s", config.TelegramCallbackKey, oauthstate.PayloadFingerprint(encodeText))
	allowed, claimErr := oauthstate.ClaimSingleUse(l.ctx, l.svcCtx.Redis, claimKey,
		time.Now(), telegramBindRetryGrace, telegramBindAuthExpire*time.Second)
	if claimErr != nil {
		l.Logger.Errorw("telegram bind replay check unavailable", zap.Any("error", claimErr.Error()))
	} else if !allowed {
		l.Logger.Errorw("telegram bind callback replayed")
		return errors.Wrap(xerr.NewErrCode(xerr.ERROR), "telegram callback has already been used")
	}

	telegramUserID := fmt.Sprintf("%d", *callbackData.Id)

	existingByOpenID, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, "telegram", telegramUserID)
	if err != nil && !ent.IsNotFound(err) {
		l.Logger.Errorw("find telegram user auth method by openid failed", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find telegram user auth method failed")
	}
	if existingByOpenID != nil && existingByOpenID.Id > 0 {
		if existingByOpenID.UserId == u.Id {
			return nil
		}
		return errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "telegram user already exists")
	}

	existingByPlatform, err := l.svcCtx.Store.User().FindUserAuthMethodByPlatform(l.ctx, u.Id, "telegram")
	if err != nil && !ent.IsNotFound(err) {
		l.Logger.Errorw("find telegram user auth method by platform failed", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find telegram user auth method failed")
	}
	if existingByPlatform.Id > 0 {
		if existingByPlatform.AuthIdentifier == telegramUserID {
			return nil
		}
		return errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "telegram already bound")
	}

	userAuthMethod := &user.AuthMethods{
		UserId:         u.Id,
		AuthType:       "telegram",
		AuthIdentifier: telegramUserID,
		Verified:       true,
	}

	err = l.svcCtx.Store.User().InsertUserAuthMethods(l.ctx, userAuthMethod)
	if err != nil {
		l.Logger.Errorw("insert telegram user auth method failed", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert telegram user auth method failed")
	}

	return nil
}
