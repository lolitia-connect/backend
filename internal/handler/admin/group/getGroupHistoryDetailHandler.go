package group

import (
	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/logic/admin/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/result"
)

// Get group history detail
func GetGroupHistoryDetailHandler(svcCtx *svc.ServiceContext) func(c *gin.Context) {
	return func(c *gin.Context) {
		var req types.GetGroupHistoryDetailRequest
		_ = c.ShouldBind(&req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := group.NewGetGroupHistoryDetailLogic(c.Request.Context(), svcCtx)
		resp, err := l.GetGroupHistoryDetail(&req)
		result.HttpResult(c, resp, err)
	}
}
