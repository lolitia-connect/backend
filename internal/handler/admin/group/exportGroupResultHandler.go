package group

import (
	"net/http"

	"github.com/perfect-panel/server/internal/logic/admin/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

// Export group result
func ExportGroupResultHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
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
		c.Writer.Header().Set("Content-Type", "text/csv")
		c.Writer.Header().Set("Content-Disposition", "attachment; filename="+filename)
		c.Writer.WriteHeader(http.StatusOK)
		_, _ = c.Writer.Write(data)
	}
}
