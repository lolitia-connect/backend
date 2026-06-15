package admin

import (
	"github.com/perfect-panel/server/internal/logic/auth/admin"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

// Generate captcha
func AdminGenerateCaptchaHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {

		l := admin.NewAdminGenerateCaptchaLogic(c.Request.Context(), svcCtx)
		resp, err := l.AdminGenerateCaptcha()
		result.HttpResult(c, resp, err)
	}
}
