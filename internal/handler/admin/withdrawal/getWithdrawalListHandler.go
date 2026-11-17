package withdrawal

import (
	logic "github.com/perfect-panel/server/internal/logic/admin/withdrawal"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

// Get Withdrawal List
func GetWithdrawalListHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		var req types.GetWithdrawalListRequest
		if err := c.ShouldBind(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		if err := svcCtx.Validate(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}

		l := logic.NewGetWithdrawalListLogic(c.Request.Context(), svcCtx)
		resp, err := l.GetWithdrawalList(&req)
		result.HttpResult(c, resp, err)
	}
}
