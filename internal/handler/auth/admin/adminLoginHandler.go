package admin

import (
	adminLogic "github.com/perfect-panel/server/internal/logic/auth/admin"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

// Admin login
func AdminLoginHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		var req types.UserLoginRequest
		_ = c.ShouldBind(&req)
		// get client ip
		req.IP = c.ClientIP()
		req.UserAgent = c.Request.UserAgent()

		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := adminLogic.NewAdminLoginLogic(c.Request.Context(), svcCtx)
		resp, err := l.AdminLogin(&req)
		result.HttpResult(c, resp, err)
	}
}
