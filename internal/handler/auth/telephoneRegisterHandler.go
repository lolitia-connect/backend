package auth

import (
	"github.com/perfect-panel/server/internal/logic/auth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

// User Telephone register
func TelephoneRegisterHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		var req types.TelephoneRegisterRequest
		if err := c.ShouldBind(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		req.IP = c.ClientIP()
		req.UserAgent = c.Request.UserAgent()
		l := auth.NewTelephoneRegisterLogic(c.Request.Context(), svcCtx)
		resp, err := l.TelephoneRegister(&req)
		result.HttpResult(c, resp, err)
	}
}
