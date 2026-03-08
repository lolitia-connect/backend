package group

import (
	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/logic/admin/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/result"
)

// Update node group
func UpdateNodeGroupHandler(svcCtx *svc.ServiceContext) func(c *gin.Context) {
	return func(c *gin.Context) {
		var req types.UpdateNodeGroupRequest
		if err := c.ShouldBindUri(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		if err := c.ShouldBind(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := group.NewUpdateNodeGroupLogic(c.Request.Context(), svcCtx)
		err := l.UpdateNodeGroup(&req)
		result.HttpResult(c, nil, err)
	}
}
