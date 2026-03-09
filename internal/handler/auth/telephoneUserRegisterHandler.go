package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/logic/auth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/result"
)

// User Telephone register
func TelephoneUserRegisterHandler(svcCtx *svc.ServiceContext) func(c *gin.Context) {
	return func(c *gin.Context) {
		var req types.TelephoneRegisterRequest
		_ = c.ShouldBind(&req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}
		// get client ip
		req.IP = c.ClientIP()
		req.UserAgent = c.Request.UserAgent()

		l := auth.NewTelephoneUserRegisterLogic(c.Request.Context(), svcCtx)
		resp, err := l.TelephoneUserRegister(&req)
		result.HttpResult(c, resp, err)
	}
}
