package server

import (
	"github.com/perfect-panel/server/internal/logic/public/server"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

// GetServerNodeListHandler Get all enabled server node list
func GetServerNodeListHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		l := server.NewGetServerNodeListLogic(c.Request.Context(), svcCtx)
		resp, err := l.GetServerNodeList()
		result.HttpResult(c, resp, err)
	}
}
