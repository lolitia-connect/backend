package admin

import (
	"github.com/gin-gonic/gin"
	adminLogic "github.com/perfect-panel/server/internal/logic/auth/admin"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/result"
)

// Admin reset password
func AdminResetPasswordHandler(svcCtx *svc.ServiceContext) func(c *gin.Context) {
	return func(c *gin.Context) {
		var req types.ResetPasswordRequest
		_ = c.ShouldBind(&req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}
		// get client ip
		req.IP = c.ClientIP()
		req.UserAgent = c.Request.UserAgent()

		l := adminLogic.NewAdminResetPasswordLogic(c.Request.Context(), svcCtx)
		resp, err := l.AdminResetPassword(&req)
		result.HttpResult(c, resp, err)
	}
}
