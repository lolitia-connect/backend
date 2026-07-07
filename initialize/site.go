package initialize

import (
	"context"

	"go.uber.org/zap"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tool"
)

func Site(ctx *svc.ServiceContext) {
	zap.S().Debug("initialize site config")
	configs, err := ctx.Store.System().GetSiteConfig(context.Background())
	if err != nil {
		panic(err)
	}
	var siteConfig config.SiteConfig
	tool.SystemConfigSliceReflectToStruct(configs, &siteConfig)
	ctx.Config.Site = siteConfig
}
