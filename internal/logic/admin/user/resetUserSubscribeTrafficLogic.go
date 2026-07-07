package user

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type ResetUserSubscribeTrafficLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewResetUserSubscribeTrafficLogic Reset user subscribe traffic
func NewResetUserSubscribeTrafficLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetUserSubscribeTrafficLogic {
	return &ResetUserSubscribeTrafficLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResetUserSubscribeTrafficLogic) ResetUserSubscribeTraffic(req *types.ResetUserSubscribeTrafficRequest) error {
	userSub, err := l.svcCtx.Store.User().FindOneSubscribe(l.ctx, req.UserSubscribeId)
	if err != nil {
		l.Logger.Errorw("FindOneSubscribe error", zap.Any("error", err.Error()), zap.Any("userSubscribeId", req.UserSubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), " FindOneSubscribe error: %v", err.Error())
	}
	userSub.Download = 0
	userSub.Upload = 0

	err = l.svcCtx.Store.User().UpdateSubscribe(l.ctx, userSub)
	if err != nil {
		l.Logger.Errorw("UpdateSubscribe error", zap.Any("error", err.Error()), zap.Any("userSubscribeId", req.UserSubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), " UpdateSubscribe error: %v", err.Error())
	}
	// Clear subscribe cache
	if err = l.svcCtx.Store.Subscribe().ClearCache(l.ctx, userSub.SubscribeId); err != nil {
		l.Logger.Errorw("failed to clear subscribe cache", zap.Any("error", err.Error()), zap.Any("subscribeId", userSub.SubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "failed to clear subscribe cache: %v", err.Error())
	}
	return nil
}
