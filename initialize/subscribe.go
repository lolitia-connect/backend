package initialize

import (
	"context"

	"go.uber.org/zap"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tool"
)

func Subscribe(svc *svc.ServiceContext) {
	zap.S().Debug("Subscribe config initialization")
	configs, err := svc.Store.System().GetSubscribeConfig(context.Background())
	if err != nil {
		zap.S().Error("[Init Subscribe Config] Get Subscribe Config Error: ", zap.Any("error", err.Error()))
		return
	}

	var subscribeConfig config.SubscribeConfig
	tool.SystemConfigSliceReflectToStruct(configs, &subscribeConfig)
	svc.Config.Subscribe = subscribeConfig
}
