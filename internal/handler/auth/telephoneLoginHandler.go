package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/logic/auth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/result"
)

// User Telephone login
func TelephoneLoginHandler(svcCtx *svc.ServiceContext) func(c *gin.Context) {
	return func(c *gin.Context) {
		var req types.TelephoneLoginRequest
		_ = c.ShouldBind(&req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}
		// get client ip
		req.IP = c.ClientIP()
		req.UserAgent = c.Request.UserAgent()

		l := auth.NewTelephoneLoginLogic(c, svcCtx)
		resp, err := l.TelephoneLogin(&req, c.Request, c.ClientIP())
		result.HttpResult(c, resp, err)
	}
}
