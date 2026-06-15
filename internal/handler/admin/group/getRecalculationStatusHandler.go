package group

import (
	"github.com/perfect-panel/server/internal/logic/admin/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

// Get recalculation status
func GetRecalculationStatusHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {

		l := group.NewGetRecalculationStatusLogic(c.Request.Context(), svcCtx)
		resp, err := l.GetRecalculationStatus()
		result.HttpResult(c, resp, err)
	}
}
