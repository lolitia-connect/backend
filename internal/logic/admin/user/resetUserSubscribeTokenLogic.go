package user

import (
	"context"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type ResetUserSubscribeTokenLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewResetUserSubscribeTokenLogic Reset user subscribe token
func NewResetUserSubscribeTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetUserSubscribeTokenLogic {
	return &ResetUserSubscribeTokenLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResetUserSubscribeTokenLogic) ResetUserSubscribeToken(req *types.ResetUserSubscribeTokenRequest) error {
	userSub, err := l.svcCtx.Store.User().FindOneSubscribe(l.ctx, req.UserSubscribeId)
	if err != nil {
		zap.S().Errorf("[ResetUserSubscribeToken] FindOneSubscribe error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneSubscribe error: %v", err.Error())
	}
	userSub.Token = uuidx.SubscribeToken(fmt.Sprintf("AdminUpdate:%d", time.Now().UnixMilli()))

	err = l.svcCtx.Store.User().UpdateSubscribe(l.ctx, userSub)
	if err != nil {
		zap.S().Errorf("[ResetUserSubscribeToken] UpdateSubscribe error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "UpdateSubscribe error: %v", err.Error())
	}
	// Clear subscribe cache
	if err = l.svcCtx.Store.Subscribe().ClearCache(l.ctx, userSub.SubscribeId); err != nil {
		l.Logger.Errorw("failed to clear subscribe cache", zap.Any("error", err.Error()), zap.Any("subscribeId", userSub.SubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "failed to clear subscribe cache: %v", err.Error())
	}
	return nil
}
