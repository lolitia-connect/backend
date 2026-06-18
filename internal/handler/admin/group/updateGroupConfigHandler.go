package group

import (
	"github.com/perfect-panel/server/internal/logic/admin/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

// Update group config
func UpdateGroupConfigHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		var req types.UpdateGroupConfigRequest
		_ = c.ShouldBind(&req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := group.NewUpdateGroupConfigLogic(c.Request.Context(), svcCtx)
		err := l.UpdateGroupConfig(&req)
		result.HttpResult(c, nil, err)
	}
}
