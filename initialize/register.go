package initialize

import (
	"context"

	"go.uber.org/zap"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tool"
)

func Register(ctx *svc.ServiceContext) {
	zap.S().Debug("Register config initialization")
	configs, err := ctx.Store.System().GetRegisterConfig(context.Background())
	if err != nil {
		zap.S().Errorf("[Init Register Config] Get Register Config Error: %s", err.Error())
		return
	}
	var registerConfig config.RegisterConfig
	tool.SystemConfigSliceReflectToStruct(configs, &registerConfig)
	ctx.Config.Register = registerConfig
}
