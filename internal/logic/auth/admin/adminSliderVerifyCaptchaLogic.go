package admin

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/captcha"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type AdminSliderVerifyCaptchaLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Verify slider captcha
func NewAdminSliderVerifyCaptchaLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminSliderVerifyCaptchaLogic {
	return &AdminSliderVerifyCaptchaLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminSliderVerifyCaptchaLogic) AdminSliderVerifyCaptcha(req *types.SliderVerifyCaptchaRequest) (resp *types.SliderVerifyCaptchaResponse, err error) {
	// Get verify config from database
	verifyCfg, err := l.svcCtx.SystemModel.GetVerifyConfig(l.ctx)
	if err != nil {
		l.Logger.Error("[AdminSliderVerifyCaptchaLogic] GetVerifyConfig error: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetVerifyConfig error: %v", err.Error())
	}

	var config struct {
		CaptchaType string `json:"captcha_type"`
	}
	tool.SystemConfigSliceReflectToStruct(verifyCfg, &config)

	if config.CaptchaType != "slider" {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "slider captcha not enabled")
	}

	sliderSvc := captcha.NewSliderService(l.svcCtx.Redis)
	token, err := sliderSvc.VerifySlider(l.ctx, req.Id, req.X, req.Y, req.Trail)
	if err != nil {
		l.Logger.Error("[AdminSliderVerifyCaptchaLogic] VerifySlider error: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "verify slider error")
	}

	return &types.SliderVerifyCaptchaResponse{
		Token: token,
	}, nil
}
