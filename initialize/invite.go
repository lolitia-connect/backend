package initialize

import (
	"context"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tool"
	"go.uber.org/zap"
)

func Invite(ctx *svc.ServiceContext) {
	// Initialize the system configuration
	zap.S().Debug("Register config initialization")
	configs, err := ctx.Store.System().GetInviteConfig(context.Background())
	if err != nil {
		zap.S().Error("[Init Invite Config] Get Invite Config Error: ", zap.Any("error", err.Error()))
		return
	}
	var inviteConfig config.InviteConfig
	tool.SystemConfigSliceReflectToStruct(configs, &inviteConfig)
	ctx.Config.Invite = inviteConfig
}
