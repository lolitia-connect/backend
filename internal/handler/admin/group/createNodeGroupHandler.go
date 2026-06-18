package group

import (
	"github.com/perfect-panel/server/internal/logic/admin/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

// Create node group
func CreateNodeGroupHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		var req types.CreateNodeGroupRequest
		_ = c.ShouldBind(&req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := group.NewCreateNodeGroupLogic(c.Request.Context(), svcCtx)
		err := l.CreateNodeGroup(&req)
		result.HttpResult(c, nil, err)
	}
}
