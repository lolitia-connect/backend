package server

import (
	"github.com/perfect-panel/server/internal/logic/admin/server"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

// SortByNameNode Sort all nodes by name
func SortByNameNodeHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		l := server.NewSortByNameNodeLogic(c.Request.Context(), svcCtx)
		err := l.SortByNameNode()
		result.HttpResult(c, nil, err)
	}
}
