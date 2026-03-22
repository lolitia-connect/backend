package admin

import (
	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/logic/auth/admin"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/result"
)

// Verify slider captcha
func AdminSliderVerifyCaptchaHandler(svcCtx *svc.ServiceContext) func(c *gin.Context) {
	return func(c *gin.Context) {
		var req types.SliderVerifyCaptchaRequest
		_ = c.ShouldBind(&req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := admin.NewAdminSliderVerifyCaptchaLogic(c.Request.Context(), svcCtx)
		resp, err := l.AdminSliderVerifyCaptcha(&req)
		result.HttpResult(c, resp, err)
	}
}
