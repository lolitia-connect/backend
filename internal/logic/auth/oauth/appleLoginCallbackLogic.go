package oauth

import (
	"context"
	"net/http"
	"net/url"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/oauthstate"
	"go.uber.org/zap"
)

type AppleLoginCallbackLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Apple Login Callback
func NewAppleLoginCallbackLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AppleLoginCallbackLogic {
	return &AppleLoginCallbackLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AppleLoginCallbackLogic) AppleLoginCallback(req *types.AppleLoginCallbackRequest, r *http.Request, w http.ResponseWriter) error {
	// validate the state code
	state, err := oauthstate.Read(l.ctx, l.svcCtx.Redis, "apple", req.State)
	if err != nil {
		l.Logger.Errorw("get apple state code from redis failed", zap.Any("error", err.Error()), zap.Any("code", req.State))
		http.Redirect(w, r, l.svcCtx.Config.Site.Host, http.StatusTemporaryRedirect)
		return nil
	}
	if err := oauthstate.ValidateRedirect(state.Redirect, l.svcCtx.Config.Site.Host); err != nil {
		return err
	}
	redirect, err := url.Parse(state.Redirect)
	if err != nil {
		return err
	}
	query := redirect.Query()
	query.Set("method", "apple")
	query.Set("code", req.Code)
	query.Set("state", req.State)
	redirect.RawQuery = query.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
	return nil
}
