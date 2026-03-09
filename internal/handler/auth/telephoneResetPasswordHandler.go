package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/logic/auth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/captcha"
	"github.com/perfect-panel/server/pkg/result"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

// Reset password
func TelephoneResetPasswordHandler(svcCtx *svc.ServiceContext) func(c *gin.Context) {
	return func(c *gin.Context) {
		var req types.TelephoneResetPasswordRequest
		_ = c.ShouldBind(&req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}
		// get client ip
		req.IP = c.ClientIP()

		// Get verify config from database
		verifyCfg, err := svcCtx.SystemModel.GetVerifyConfig(c.Request.Context())
		if err != nil {
			result.HttpResult(c, nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get verify config failed: %v", err))
			return
		}

		var config struct {
			CaptchaType                     string `json:"captcha_type"`
			EnableUserResetPasswordCaptcha bool   `json:"enable_user_reset_password_captcha"`
			TurnstileSecret                 string `json:"turnstile_secret"`
		}
		tool.SystemConfigSliceReflectToStruct(verifyCfg, &config)

		// Verify captcha if enabled
		if config.EnableUserResetPasswordCaptcha {
			captchaService := captcha.NewService(captcha.Config{
				Type:            captcha.CaptchaType(config.CaptchaType),
				TurnstileSecret: config.TurnstileSecret,
				RedisClient:     svcCtx.Redis,
			})

			var token, code string
			if config.CaptchaType == "turnstile" {
				token = req.CfToken
			} else {
				token = req.CaptchaId
				code = req.CaptchaCode
			}

			verified, err := captchaService.Verify(c.Request.Context(), token, code, req.IP)
			if err != nil || !verified {
				result.HttpResult(c, nil, errors.Wrapf(xerr.NewErrCode(xerr.TooManyRequests), "captcha verification failed: %v", err))
				return
			}
		}

		l := auth.NewTelephoneResetPasswordLogic(c, svcCtx)
		resp, err := l.TelephoneResetPassword(&req)
		result.HttpResult(c, resp, err)
	}
}
