package group

import (
	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/logic/admin/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/result"
)

// Preview user nodes
func PreviewUserNodesHandler(svcCtx *svc.ServiceContext) func(c *gin.Context) {
	return func(c *gin.Context) {
		var req types.PreviewUserNodesRequest
		_ = c.ShouldBind(&req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := group.NewPreviewUserNodesLogic(c.Request.Context(), svcCtx)
		resp, err := l.PreviewUserNodes(&req)
		result.HttpResult(c, resp, err)
	}
}
