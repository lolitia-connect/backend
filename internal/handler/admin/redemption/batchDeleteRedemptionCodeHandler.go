package redemption

import (
	"github.com/perfect-panel/server/internal/logic/admin/redemption"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

// Batch delete redemption code
func BatchDeleteRedemptionCodeHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		var req types.BatchDeleteRedemptionCodeRequest
		if err := c.ShouldBind(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := redemption.NewBatchDeleteRedemptionCodeLogic(c.Request.Context(), svcCtx)
		err := l.BatchDeleteRedemptionCode(&req)
		result.HttpResult(c, nil, err)
	}
}
