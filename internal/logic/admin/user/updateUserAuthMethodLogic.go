package user

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdateUserAuthMethodLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update user auth method
func NewUpdateUserAuthMethodLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserAuthMethodLogic {
	return &UpdateUserAuthMethodLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateUserAuthMethodLogic) UpdateUserAuthMethod(req *types.UpdateUserAuthMethodRequest) error {
	method, err := l.svcCtx.Store.User().FindUserAuthMethodByPlatform(l.ctx, req.UserId, req.AuthType)
	if err != nil {
		l.Logger.Errorw("Get user auth method error", zap.Any("error", err.Error()), zap.Any("userId", req.UserId), zap.Any("authType", req.AuthType))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Get user auth method error: %v", err.Error())
	}
	method.AuthType = req.AuthType
	method.AuthIdentifier = req.AuthIdentifier
	if err = l.svcCtx.Store.User().UpdateUserAuthMethods(l.ctx, method); err != nil {
		l.Logger.Errorw("Update user auth method error", zap.Any("error", err.Error()), zap.Any("userId", req.UserId), zap.Any("authType", req.AuthType))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "Update user auth method error: %v", err.Error())
	}
	return nil
}
