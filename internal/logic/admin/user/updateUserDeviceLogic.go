package user

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdateUserDeviceLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// User device
func NewUpdateUserDeviceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserDeviceLogic {
	return &UpdateUserDeviceLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateUserDeviceLogic) UpdateUserDevice(req *types.UserDevice) error {
	device, err := l.svcCtx.Store.User().FindOneDevice(l.ctx, req.Id)
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get Device  error: %v", err.Error())
	}
	device.Enabled = req.Enabled
	err = l.svcCtx.Store.User().UpdateDevice(l.ctx, device)
	if err != nil {
		l.Logger.Error("[UpdateUserDeviceLogic] Update Device Error:", zap.Any("err", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update Device error: %v", err.Error())
	}
	return nil
}
