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

type GenerateCaptchaLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Generate captcha
func NewGenerateCaptchaLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GenerateCaptchaLogic {
	return &GenerateCaptchaLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GenerateCaptchaLogic) GenerateCaptcha() (resp *types.GenerateCaptchaResponse, err error) {
	resp = &types.GenerateCaptchaResponse{}

	// Get verify config from database
	verifyCfg, err := l.svcCtx.SystemModel.GetVerifyConfig(l.ctx)
	if err != nil {
		l.Logger.Error("[GenerateCaptchaLogic] GetVerifyConfig error: ", logger.Field("error", err.Error()))
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
			l.Logger.Error("[GenerateCaptchaLogic] Generate captcha error: ", logger.Field("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Generate captcha error: %v", err.Error())
		}

		resp.Id = id
		resp.Image = image
	} else if config.CaptchaType == "turnstile" {
		// For Turnstile, just return the site key
		resp.Id = config.TurnstileSiteKey
	}

	return resp, nil
}
