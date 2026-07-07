package system

import (
	"context"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdateVerifyConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateVerifyConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateVerifyConfigLogic {
	return &UpdateVerifyConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateVerifyConfigLogic) UpdateVerifyConfig(req *types.VerifyConfig) error {
	err := updateConfigFields(l.ctx, l.svcCtx, "verify", convertedConfigFields(*req), config.VerifyConfigKey, config.GlobalConfigKey)
	if err != nil {
		l.Logger.Errorw("[UpdateVerifyConfigLogic] update verify config error: ", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update verify config error: %v", err)
	}
	// Update the config
	tool.DeepCopy(&l.svcCtx.Config.Verify, req)
	initialize.Verify(l.svcCtx)
	return nil
}
