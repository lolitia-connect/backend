package group

import (
	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/logic/admin/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// Reset all groups
func ResetGroupsHandler(svcCtx *svc.ServiceContext) func(c *gin.Context) {
	return func(c *gin.Context) {
		l := group.NewResetGroupsLogic(c.Request.Context(), svcCtx)
		err := l.ResetGroups()
		result.HttpResult(c, nil, err)
	}
}
