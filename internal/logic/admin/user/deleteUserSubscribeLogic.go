package user

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type DeleteUserSubscribeLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewDeleteUserSubscribeLogic Delete user subcribe
func NewDeleteUserSubscribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteUserSubscribeLogic {
	return &DeleteUserSubscribeLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteUserSubscribeLogic) DeleteUserSubscribe(req *types.DeleteUserSubscribeRequest) error {
	// find user subscribe by ID
	userSubscribe, err := l.svcCtx.Store.User().FindOneSubscribe(l.ctx, req.UserSubscribeId)
	if err != nil {
		l.Logger.Errorw("failed to find user subscribe", zap.Any("error", err.Error()), zap.Any("userSubscribeId", req.UserSubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "failed to find user subscribe: %v", err.Error())
	}

	err = l.svcCtx.Store.User().DeleteSubscribeById(l.ctx, req.UserSubscribeId)
	if err != nil {
		l.Logger.Errorw("failed to delete user subscribe", zap.Any("error", err.Error()), zap.Any("userSubscribeId", req.UserSubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "failed to delete user subscribe: %v", err.Error())
	}
	// Clear subscribe cache
	if err = l.svcCtx.Store.Subscribe().ClearCache(l.ctx, userSubscribe.SubscribeId); err != nil {
		l.Logger.Errorw("failed to clear subscribe cache", zap.Any("error", err.Error()), zap.Any("subscribeId", userSubscribe.SubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "failed to clear subscribe cache: %v", err.Error())
	}
	if err = l.svcCtx.Store.Node().ClearServerAllCache(l.ctx); err != nil {
		l.Logger.Errorf("ClearServerAllCache error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "failed to clear server cache: %v", err.Error())
	}
	return nil
}
