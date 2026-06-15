package server

import (
	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/logic/public/server"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/result"
)

// GetServerNodeListHandler Get all enabled server node list
func GetServerNodeListHandler(svcCtx *svc.ServiceContext) func(c *gin.Context) {
	return func(c *gin.Context) {
		l := server.NewGetServerNodeListLogic(c.Request.Context(), svcCtx)
		resp, err := l.GetServerNodeList()
		result.HttpResult(c, resp, err)
	}
}
