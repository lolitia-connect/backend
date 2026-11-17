package withdrawal

import (
	logic "github.com/perfect-panel/server/internal/logic/admin/withdrawal"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

// Review Withdrawal
func ReviewWithdrawalHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		var req types.ReviewWithdrawalRequest
		if err := c.ShouldBind(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		if err := svcCtx.Validate(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}

		l := logic.NewReviewWithdrawalLogic(c.Request.Context(), svcCtx)
		err := l.ReviewWithdrawal(&req)
		result.HttpResult(c, nil, err)
	}
}
