package user

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type ToggleUserSubscribeStatusLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewToggleUserSubscribeStatusLogic Stop user subscribe
func NewToggleUserSubscribeStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ToggleUserSubscribeStatusLogic {
	return &ToggleUserSubscribeStatusLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ToggleUserSubscribeStatusLogic) ToggleUserSubscribeStatus(req *types.ToggleUserSubscribeStatusRequest) error {
	userSub, err := l.svcCtx.Store.User().FindOneSubscribe(l.ctx, req.UserSubscribeId)
	if err != nil {
		l.Logger.Errorw("FindOneSubscribe error", zap.Any("error", err.Error()), zap.Any("userSubscribeId", req.UserSubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), " FindOneSubscribe error: %v", err.Error())
	}

	switch userSub.Status {
	case 1: // active
		userSub.Status = 5 // set status to stopped
	case 5: // stopped
		userSub.Status = 1 // set status to active
	default:
		l.Logger.Errorw("invalid user subscribe status", zap.Any("userSubscribeId", req.UserSubscribeId), zap.Any("status", userSub.Status))
		return errors.Wrapf(xerr.NewErrCodeMsg(xerr.ERROR, "invalid subscribe status"), "invalid user subscribe status: %d", userSub.Status)
	}

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
