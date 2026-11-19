package user

import (
	"github.com/perfect-panel/server/internal/logic/public/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/internal/usersession"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

// Update User Password
func UpdateUserPasswordHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		var req types.UpdateUserPasswordRequest
		if err := c.ShouldBind(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := user.NewUpdateUserPasswordLogic(c.Request.Context(), svcCtx)
		err := l.UpdateUserPassword(&req)
		result.HttpResult(c, nil, err)
	}
}

func LogoutHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		sessionID, _ := c.Request.Context().Value(constant.CtxKeySessionID).(string)
		result.HttpResult(c, nil, usersession.End(c.Request.Context(), svcCtx.Redis, sessionID))
	}
}
