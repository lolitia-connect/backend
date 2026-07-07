package user

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdateUserNotifySettingLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUpdateUserNotifySettingLogic Update user notify setting
func NewUpdateUserNotifySettingLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserNotifySettingLogic {
	return &UpdateUserNotifySettingLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateUserNotifySettingLogic) UpdateUserNotifySetting(req *types.UpdateUserNotifySettingRequest) error {
	userInfo, err := l.svcCtx.Store.User().FindOne(l.ctx, req.UserId)
	if err != nil {
		l.Logger.Errorw("[UpdateUserNotifySettingLogic] Find User Error:", zap.Any("err", err.Error()), zap.Any("userId", req.UserId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Find User Error")
	}
	tool.DeepCopy(userInfo, req)
	err = l.svcCtx.Store.User().Update(l.ctx, userInfo)
	if err != nil {
		l.Logger.Errorw("[UpdateUserNotifySettingLogic] Update User Error:", zap.Any("err", err.Error()), zap.Any("userId", req.UserId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "Update User Error")
	}
	return nil
}
