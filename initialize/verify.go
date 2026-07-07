package initialize

import (
	"context"

	"go.uber.org/zap"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tool"
)

type verifyConfig struct {
	TurnstileSiteKey          string
	TurnstileSecret           string
	EnableLoginVerify         bool
	EnableRegisterVerify      bool
	EnableResetPasswordVerify bool
}

func Verify(svc *svc.ServiceContext) {
	zap.S().Debug("Verify config initialization")
	configs, err := svc.Store.System().GetVerifyConfig(context.Background())
	if err != nil {
		zap.S().Error("[Init Verify Config] Get Verify Config Error: ", zap.Any("error", err.Error()))
		return
	}
	var verify verifyConfig
	tool.SystemConfigSliceReflectToStruct(configs, &verify)
	svc.Config.Verify = config.Verify{
		TurnstileSiteKey:    verify.TurnstileSiteKey,
		TurnstileSecret:     verify.TurnstileSecret,
		LoginVerify:         verify.EnableLoginVerify,
		RegisterVerify:      verify.EnableRegisterVerify,
		ResetPasswordVerify: verify.EnableResetPasswordVerify,
	}

	zap.S().Debug("Verify code config initialization")

	var verifyCodeConfig config.VerifyCode
	cfg, err := svc.Store.System().GetVerifyCodeConfig(context.Background())
	if err != nil {
		zap.S().Errorf("[Init Verify Config] Get Verify Code Config Error: %s", err.Error())
		return
	}
	tool.SystemConfigSliceReflectToStruct(cfg, &verifyCodeConfig)
	svc.Config.VerifyCode = verifyCodeConfig
}
