package subscribe

import (
	"context"

	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdateSubscribeGroupLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update subscribe group
func NewUpdateSubscribeGroupLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateSubscribeGroupLogic {
	return &UpdateSubscribeGroupLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateSubscribeGroupLogic) UpdateSubscribeGroup(req *types.UpdateSubscribeGroupRequest) error {
	err := l.svcCtx.Store.Subscribe().UpdateGroup(l.ctx, &subscribe.Group{
		Id:          req.Id,
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		l.Logger.Error("[UpdateSubscribeGroup] update subscribe group failed", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update subscribe group failed: %v", err.Error())
	}
	return nil
}
