package admin

import (
	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/logic/auth/admin"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Generate captcha
func AdminGenerateCaptchaHandler(svcCtx *svc.ServiceContext) func(c *gin.Context) {
	return func(c *gin.Context) {

		l := admin.NewAdminGenerateCaptchaLogic(c.Request.Context(), svcCtx)
		resp, err := l.AdminGenerateCaptcha()
		result.HttpResult(c, resp, err)
	}
}
