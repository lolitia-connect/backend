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

type AdminGenerateCaptchaLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Generate captcha
func NewAdminGenerateCaptchaLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdminGenerateCaptchaLogic {
	return &AdminGenerateCaptchaLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdminGenerateCaptchaLogic) AdminGenerateCaptcha() (resp *types.GenerateCaptchaResponse, err error) {
	resp = &types.GenerateCaptchaResponse{}

	// Get verify config from database
	verifyCfg, err := l.svcCtx.SystemModel.GetVerifyConfig(l.ctx)
	if err != nil {
		l.Logger.Error("[AdminGenerateCaptchaLogic] GetVerifyConfig error: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetVerifyConfig error: %v", err.Error())
	}

	var config struct {
		CaptchaType      string `json:"captcha_type"`
		TurnstileSiteKey string `json:"turnstile_site_key"`
		TurnstileSecret  string `json:"turnstile_secret"`
	}
	tool.SystemConfigSliceReflectToStruct(verifyCfg, &config)

	resp.Type = config.CaptchaType

	// If captcha type is local, generate captcha image
	if config.CaptchaType == "local" {
		captchaService := captcha.NewService(captcha.Config{
			Type:        captcha.CaptchaTypeLocal,
			RedisClient: l.svcCtx.Redis,
		})

		id, image, err := captchaService.Generate(l.ctx)
		if err != nil {
			l.Logger.Error("[AdminGenerateCaptchaLogic] Generate captcha error: ", logger.Field("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Generate captcha error: %v", err.Error())
		}

		resp.Id = id
		resp.Image = image
	} else if config.CaptchaType == "turnstile" {
		// For Turnstile, just return the site key
		resp.Id = config.TurnstileSiteKey
	} else if config.CaptchaType == "slider" {
		// For slider, generate background and block images
		sliderSvc := captcha.NewSliderService(l.svcCtx.Redis)
		id, bgImage, blockImage, err := sliderSvc.GenerateSlider(l.ctx)
		if err != nil {
			l.Logger.Error("[AdminGenerateCaptchaLogic] Generate slider captcha error: ", logger.Field("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Generate slider captcha error: %v", err.Error())
		}
		resp.Id = id
		resp.Image = bgImage
		resp.BlockImage = blockImage
	}

	return resp, nil
}
