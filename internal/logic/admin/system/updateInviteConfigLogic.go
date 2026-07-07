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

type UpdateInviteConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateInviteConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateInviteConfigLogic {
	return &UpdateInviteConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateInviteConfigLogic) UpdateInviteConfig(req *types.InviteConfig) error {
	err := updateConfigFields(l.ctx, l.svcCtx, "invite", convertedConfigFields(*req), config.InviteConfigKey, config.GlobalConfigKey)
	if err != nil {
		l.Logger.Errorw("[UpdateInviteConfig] update invite config error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update invite config error: %v", err)
	}
	initialize.Invite(l.svcCtx)
	return nil
}
