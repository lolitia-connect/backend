package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
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
	result, err := l.svcCtx.Redis.Get(l.ctx, fmt.Sprintf("apple:%s", req.State)).Result()
	if err != nil {
		l.Logger.Errorw("get apple state code from redis failed", zap.Any("error", err.Error()), zap.Any("code", req.State))
		http.Redirect(w, r, l.svcCtx.Config.Site.Host, http.StatusTemporaryRedirect)
		return nil
	}
	http.Redirect(w, r, fmt.Sprintf("%s?method=apple&code=%s&state=%s", result, req.Code, req.State), http.StatusFound)
	l.Logger.Infow("redirect to apple login page", zap.Any("url", fmt.Sprintf("%s?method=apple&code=%s&state=%s", result, url.QueryEscape(req.Code), req.State)))
	return nil
}
