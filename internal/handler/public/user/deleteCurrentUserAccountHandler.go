package user

import (
	"github.com/perfect-panel/server/internal/logic/public/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

// Delete Current User Account
func DeleteCurrentUserAccountHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {

		l := user.NewDeleteCurrentUserAccountLogic(c.Request.Context(), svcCtx)
		err := l.DeleteCurrentUserAccount()
		result.HttpResult(c, nil, err)
	}
}
