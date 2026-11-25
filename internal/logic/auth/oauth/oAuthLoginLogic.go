package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/perfect-panel/server/internal/model/auth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/oauth/facebook"
	"github.com/perfect-panel/server/pkg/oauth/github"
	"github.com/perfect-panel/server/pkg/oauth/google"
	"github.com/perfect-panel/server/pkg/oauth/telegram"
	"github.com/perfect-panel/server/pkg/oauthstate"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

type OAuthLoginLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// OAuth login
func NewOAuthLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OAuthLoginLogic {
	return &OAuthLoginLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OAuthLoginLogic) OAuthLogin(req *types.OAthLoginRequest) (resp *types.OAuthLoginResponse, err error) {
	if req.Method == "apple" || req.Method == "telegram" {
		if err := oauthstate.ValidateRedirect(req.Redirect, l.svcCtx.Config.Site.Host); err != nil {
			return nil, err
		}
	}
	var uri string
	switch req.Method {
	case "google":
		uri, err = l.google(req)
	case "apple":
		uri, err = l.apple(req)
	case "telegram":
		uri, err = l.telegram(req)
	case "github":
		uri, err = l.github()
	case "facebook":
		uri, err = l.facebook()
	default:
		return nil, xerr.NewErrCode(xerr.InvalidParams)
	}
	if err != nil {
		l.Logger.Errorw("OAuthLogin ", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "OAuthLogin: %v", err.Error())
	}
	return &types.OAuthLoginResponse{
		Redirect: uri,
	}, nil
}

func (l *OAuthLoginLogic) google(req *types.OAthLoginRequest) (string, error) {
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "google")
	if err != nil {
		return "", err
	}
	var cfg auth.GoogleAuthConfig
	err = json.Unmarshal([]byte(authMethod.Config), &cfg)
	if err != nil {
		l.Logger.Errorw("error unmarshal google config: %v", zap.Any("config", authMethod.Config), zap.Any("error", err.Error()))
		return "", err
	}
	client := google.New(&google.Config{
		ClientID:     cfg.ClientId,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  req.Redirect,
	})
	// generate the state code
	code, err := oauthstate.Issue(l.ctx, l.svcCtx.Redis, "google", "login", 0, req.Redirect)
	if err != nil {
		return "", err
	}
	uri := client.AuthCodeURL(code, oauth2.AccessTypeOffline)
	return uri, nil
}

func (l *OAuthLoginLogic) facebook() (string, error) {
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "facebook")
	if err != nil {
		return "", err
	}
	var cfg auth.FacebookAuthConfig
	if err = json.Unmarshal([]byte(authMethod.Config), &cfg); err != nil {
		l.Logger.Errorw("error unmarshal facebook config", zap.Any("config", authMethod.Config), zap.Any("error", err.Error()))
		return "", err
	}
	client := facebook.New(&facebook.Config{
		ClientID:     cfg.ClientId,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
	})
	code, err := oauthstate.Issue(l.ctx, l.svcCtx.Redis, "facebook", "login", 0, cfg.RedirectURL)
	if err != nil {
		return "", err
	}
	return client.AuthCodeURL(code, oauth2.AccessTypeOffline), nil
}
func (l *OAuthLoginLogic) apple(req *types.OAthLoginRequest) (string, error) {
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "apple")
	if err != nil {
		return "", err
	}
	var cfg auth.AppleAuthConfig
	err = json.Unmarshal([]byte(authMethod.Config), &cfg)
	if err != nil {
		l.Logger.Errorw("error unmarshal apple config: %v", zap.Any("config", authMethod.Config), zap.Any("error", err.Error()))
		return "", err
	}
	uri := "https://appleid.apple.com/auth/authorize?client_id=%s&redirect_uri=%s&response_type=code&state=%s&scope=name email&response_mode=form_post"
	// generate the state code
	code, err := oauthstate.Issue(l.ctx, l.svcCtx.Redis, "apple", "login", 0, req.Redirect)
	if err != nil {
		l.Logger.Errorw("error save state code to redis: %v", zap.Any("code", code), zap.Any("error", err.Error()))
		return "", err
	}
	return fmt.Sprintf(uri, url.QueryEscape(cfg.ClientId), url.QueryEscape(cfg.CallbackURL()), code), nil
}
func (l *OAuthLoginLogic) github() (string, error) {
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "github")
	if err != nil {
		return "", err
	}
	var cfg auth.GithubAuthConfig
	if err = json.Unmarshal([]byte(authMethod.Config), &cfg); err != nil {
		l.Logger.Errorw("error unmarshal github config", zap.Any("config", authMethod.Config), zap.Any("error", err.Error()))
		return "", err
	}
	client := github.New(&github.Config{
		ClientID:     cfg.ClientId,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
	})
	code, err := oauthstate.Issue(l.ctx, l.svcCtx.Redis, "github", "login", 0, cfg.RedirectURL)
	if err != nil {
		return "", err
	}
	return client.AuthCodeURL(code, oauth2.AccessTypeOffline), nil
}
func (l *OAuthLoginLogic) telegram(req *types.OAthLoginRequest) (string, error) {
	authMethod, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, "telegram")
	if err != nil {
		return "", err
	}
	var cfg auth.TelegramAuthConfig
	err = json.Unmarshal([]byte(authMethod.Config), &cfg)
	if err != nil {
		l.Logger.Errorw("error unmarshal apple config: %v", zap.Any("config", authMethod.Config), zap.Any("error", err.Error()))
		return "", err
	}
	// generate the state code
	code, err := oauthstate.Issue(l.ctx, l.svcCtx.Redis, "telegram", "login", 0, req.Redirect)
	if err != nil {
		l.Logger.Errorw("error save state code to redis", zap.Any("code", code), zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "error save state code to redis")
	}
	return telegram.GenerateTelegramOAuthURL(cfg.BotToken, code, req.Redirect), nil
}
