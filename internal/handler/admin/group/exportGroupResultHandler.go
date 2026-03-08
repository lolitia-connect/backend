package group

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/logic/admin/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/result"
)

// Export group result
func ExportGroupResultHandler(svcCtx *svc.ServiceContext) func(c *gin.Context) {
	return func(c *gin.Context) {
		var req types.ExportGroupResultRequest
		_ = c.ShouldBind(&req)
		validateErr := svcCtx.Validate(&req)
		if validateErr != nil {
			result.ParamErrorResult(c, validateErr)
			return
		}

		l := group.NewExportGroupResultLogic(c.Request.Context(), svcCtx)
		data, filename, err := l.ExportGroupResult(&req)
		if err != nil {
			result.HttpResult(c, nil, err)
			return
		}

		// 设置响应头
		c.Header("Content-Type", "text/csv")
		c.Header("Content-Disposition", "attachment; filename="+filename)
		c.Data(http.StatusOK, "text/csv", data)
	}
}
