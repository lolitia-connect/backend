package user

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdateUserSubscribeLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUpdateUserSubscribeLogic Update user subscribe
func NewUpdateUserSubscribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserSubscribeLogic {
	return &UpdateUserSubscribeLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateUserSubscribeLogic) UpdateUserSubscribe(req *types.UpdateUserSubscribeRequest) error {
	userSub, err := l.svcCtx.Store.User().FindOneSubscribe(l.ctx, req.UserSubscribeId)
	if err != nil {
		l.Logger.Errorw("FindOneUserSubscribe failed:", zap.Any("error", err.Error()), zap.Any("userSubscribeId", req.UserSubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneUserSubscribe failed: %v", err.Error())
	}
	expiredAt := time.UnixMilli(req.ExpiredAt)
	if req.ExpiredAt != 0 && time.Since(expiredAt).Minutes() > 0 {
		userSub.Status = 3
	} else {
		userSub.Status = 1
	}

	err = l.svcCtx.Store.User().UpdateSubscribe(l.ctx, &user.Subscribe{
		Id:               userSub.Id,
		UserId:           userSub.UserId,
		OrderId:          userSub.OrderId,
		SubscribeId:      req.SubscribeId,
		StartTime:        userSub.StartTime,
		ExpireTime:       time.UnixMilli(req.ExpiredAt),
		Traffic:          req.Traffic,
		TrafficUnlimited: req.TrafficUnlimited,
		Download:         req.Download,
		Upload:           req.Upload,
		Token:            userSub.Token,
		UUID:             userSub.UUID,
		Status:           userSub.Status,
		NodeGroupId:      userSub.NodeGroupId,
	})

	if err != nil {
		l.Logger.Errorw("UpdateSubscribe failed:", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "UpdateSubscribe failed: %v", err.Error())
	}
	// Clear subscribe cache
	if err = l.svcCtx.Store.Subscribe().ClearCache(l.ctx, userSub.SubscribeId); err != nil {
		l.Logger.Errorw("failed to clear subscribe cache", zap.Any("error", err.Error()), zap.Any("subscribeId", userSub.SubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "failed to clear subscribe cache: %v", err.Error())
	}

	if err = l.svcCtx.Store.Node().ClearServerAllCache(l.ctx); err != nil {
		l.Logger.Errorf("ClearServerAllCache error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "failed to clear server cache: %v", err.Error())
	}

	return nil
}
