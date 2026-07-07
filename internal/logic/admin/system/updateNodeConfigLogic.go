package system

import (
	"context"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
)

type UpdateNodeConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateNodeConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateNodeConfigLogic {
	return &UpdateNodeConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateNodeConfigLogic) UpdateNodeConfig(req *types.NodeConfig) error {
	err := updateConfigFields(l.ctx, l.svcCtx, "server", convertedConfigFields(*req), config.NodeConfigKey)
	if err != nil {
		l.Logger.Errorw("[UpdateNodeConfig] update node config error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update server config error: %v", err)
	}
	initialize.Node(l.svcCtx)
	return nil
}
