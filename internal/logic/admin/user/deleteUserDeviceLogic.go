package user

import (
	"context"

	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type DeleteUserDeviceLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Delete user device
func NewDeleteUserDeviceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteUserDeviceLogic {
	return &DeleteUserDeviceLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteUserDeviceLogic) DeleteUserDevice(req *types.DeleteUserDeivceRequest) error {
	err := l.svcCtx.Store.User().DeleteDevice(l.ctx, req.Id)
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete user error: %v", err.Error())
	}
	return nil
}
