package server

import (
	"github.com/perfect-panel/server/internal/logic/admin/server"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

// SortByNameServer Sort all servers by name
func SortByNameServerHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		l := server.NewSortByNameServerLogic(c.Request.Context(), svcCtx)
		err := l.SortByNameServer()
		result.HttpResult(c, nil, err)
	}
}
