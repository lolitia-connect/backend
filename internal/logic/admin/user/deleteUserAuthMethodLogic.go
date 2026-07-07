package user

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type DeleteUserAuthMethodLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Delete user auth method
func NewDeleteUserAuthMethodLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteUserAuthMethodLogic {
	return &DeleteUserAuthMethodLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteUserAuthMethodLogic) DeleteUserAuthMethod(req *types.DeleteUserAuthMethodRequest) error {
	err := l.svcCtx.Store.User().DeleteUserAuthMethods(l.ctx, req.UserId, req.AuthType)
	if err != nil {
		l.Logger.Errorw("[DeleteUserAuthMethodLogic] Delete User Auth Method Error:", zap.Any("err", err.Error()), zap.Any("userId", req.UserId), zap.Any("authType", req.AuthType))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "Delete User Auth Method Error")
	}
	return nil
}
