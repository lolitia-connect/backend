package common

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/report"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetGlobalConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get global config
func NewGetGlobalConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetGlobalConfigLogic {
	return &GetGlobalConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetGlobalConfigLogic) GetGlobalConfig() (resp *types.GetGlobalConfigResponse, err error) {
	resp = new(types.GetGlobalConfigResponse)

	currencyCfg, err := l.svcCtx.Store.System().GetCurrencyConfig(l.ctx)
	if err != nil {
		l.Logger.Error("[GetGlobalConfigLogic] GetCurrencyConfig error: ", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetCurrencyConfig error: %v", err.Error())
	}
	verifyCodeCfg, err := l.svcCtx.Store.System().GetVerifyCodeConfig(l.ctx)
	if err != nil {
		l.Logger.Error("[GetGlobalConfigLogic] GetVerifyCodeConfig error: ", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetVerifyCodeConfig error: %v", err.Error())
	}
	verifyCfg, err := l.svcCtx.Store.System().GetVerifyConfig(l.ctx)
	if err != nil {
		l.Logger.Error("[GetGlobalConfigLogic] GetVerifyConfig error: ", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetVerifyConfig error: %v", err.Error())
	}

	tool.DeepCopy(&resp.Site, l.svcCtx.Config.Site)
	tool.DeepCopy(&resp.Subscribe, l.svcCtx.Config.Subscribe)
	tool.DeepCopy(&resp.Auth.Email, l.svcCtx.Config.Email)
	tool.DeepCopy(&resp.Auth.Mobile, l.svcCtx.Config.Mobile)
	tool.DeepCopy(&resp.Auth.Register, l.svcCtx.Config.Register)
	tool.DeepCopy(&resp.Invite, l.svcCtx.Config.Invite)
	tool.SystemConfigSliceReflectToStruct(currencyCfg, &resp.Currency)
	tool.SystemConfigSliceReflectToStruct(verifyCodeCfg, &resp.VerifyCode)
	tool.SystemConfigSliceReflectToStruct(verifyCfg, &resp.Verify)

	if report.IsGatewayMode() {
		resp.Subscribe.SubscribePath = "/sub" + l.svcCtx.Config.Subscribe.SubscribePath
	}

	var methods []string

	// auth methods
	authMethods, err := l.svcCtx.Store.Auth().FindAll(l.ctx)
	if err != nil {
		l.Logger.Error("[GetGlobalConfigLogic] FindAll error: ", zap.Any("error", err.Error()))
	}

	for _, method := range authMethods {
		if *method.Enabled {
			methods = append(methods, method.Method)
			if method.Method == "device" {
				_ = json.Unmarshal([]byte(method.Config), &resp.Auth.Device)
				resp.Auth.Device.Enable = true
			}
		}
	}
	resp.OAuthMethods = methods

	webAds, err := l.svcCtx.Store.System().FindOneByKey(l.ctx, "WebAD")
	if err != nil {
		l.Logger.Error("[GetGlobalConfigLogic] FindOneByKey error: ", zap.Any("error", err.Error()), zap.Any("key", "WebAD"))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneByKey error: %v", err.Error())
	}
	// web ads config
	resp.WebAd = webAds.Value == "true"
	return
}
