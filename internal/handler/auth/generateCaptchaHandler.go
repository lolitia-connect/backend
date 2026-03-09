package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/logic/auth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Generate captcha
func GenerateCaptchaHandler(svcCtx *svc.ServiceContext) func(c *gin.Context) {
	return func(c *gin.Context) {

		l := auth.NewGenerateCaptchaLogic(c.Request.Context(), svcCtx)
		resp, err := l.GenerateCaptcha()
		result.HttpResult(c, resp, err)
	}
}
