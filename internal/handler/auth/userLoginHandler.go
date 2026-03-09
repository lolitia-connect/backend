package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/logic/auth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/result"
)

// User login
func UserLoginHandler(svcCtx *svc.ServiceContext) func(c *gin.Context) {
	return func(c *gin.Context) {
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

		l := auth.NewUserLoginLogic(c.Request.Context(), svcCtx)
		resp, err := l.UserLogin(&req)
		result.HttpResult(c, resp, err)
	}
}
