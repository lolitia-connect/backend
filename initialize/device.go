package initialize

import (
	"context"

	"go.uber.org/zap"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/auth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tool"
)

func Device(ctx *svc.ServiceContext) {
	zap.S().Debug("device config initialization")
	method, err := ctx.Store.Auth().FindOneByMethod(context.Background(), "device")
	if err != nil {
		panic(err)
	}
	var cfg config.DeviceConfig
	var deviceConfig auth.DeviceConfig
	deviceConfig.Unmarshal(method.Config)
	tool.DeepCopy(&cfg, deviceConfig)
	cfg.Enable = *method.Enabled
	ctx.Config.Device = cfg
}
