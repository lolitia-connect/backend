package system

import (
	"context"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdateSubscribeConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateSubscribeConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateSubscribeConfigLogic {
	return &UpdateSubscribeConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateSubscribeConfigLogic) UpdateSubscribeConfig(req *types.SubscribeConfig) error {
	err := updateConfigFields(l.ctx, l.svcCtx, "subscribe", convertedConfigFields(*req), config.SubscribeConfigKey, config.GlobalConfigKey)

	if err != nil {
		l.Logger.Errorw("[UpdateSubscribeConfigLogic] update subscribe config error: ", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update subscribe config error: %v", err)
	}

	if l.svcCtx.Config.Subscribe.SubscribePath != req.SubscribePath {
		go func(svc *svc.ServiceContext) {
			err = svc.Restart()
			if err != nil {
				l.Logger.Errorw("[UpdateSubscribeConfigLogic] restart error: ", zap.Any("error", err.Error()))
			}
		}(l.svcCtx)
		return nil
	}

	initialize.Subscribe(l.svcCtx)
	return nil
}
