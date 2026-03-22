package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/logic/auth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/result"
)

// Verify slider captcha
func SliderVerifyCaptchaHandler(svcCtx *svc.ServiceContext) func(c *gin.Context) {
	return func(c *gin.Context) {
		var req types.SliderVerifyCaptchaRequest
		_ = c.ShouldBind(&req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := auth.NewSliderVerifyCaptchaLogic(c.Request.Context(), svcCtx)
		resp, err := l.SliderVerifyCaptcha(&req)
		result.HttpResult(c, resp, err)
	}
}
