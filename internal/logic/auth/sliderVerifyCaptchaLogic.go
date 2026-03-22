package auth

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

type SliderVerifyCaptchaLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Verify slider captcha
func NewSliderVerifyCaptchaLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SliderVerifyCaptchaLogic {
	return &SliderVerifyCaptchaLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SliderVerifyCaptchaLogic) SliderVerifyCaptcha(req *types.SliderVerifyCaptchaRequest) (resp *types.SliderVerifyCaptchaResponse, err error) {
	var config struct {
		CaptchaType string `json:"captcha_type"`
	}
	verifyCfg, err := l.svcCtx.SystemModel.GetVerifyConfig(l.ctx)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetVerifyConfig error: %v", err.Error())
	}
	tool.SystemConfigSliceReflectToStruct(verifyCfg, &config)

	if config.CaptchaType != string(captcha.CaptchaTypeSlider) {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "slider captcha not enabled")
	}

	sliderSvc := captcha.NewSliderService(l.svcCtx.Redis)
	token, err := sliderSvc.VerifySlider(l.ctx, req.Id, req.X, req.Y, req.Trail)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "slider verify failed: %v", err.Error())
	}

	return &types.SliderVerifyCaptchaResponse{Token: token}, nil
}
