package user

import (
	"context"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/google/uuid"
	"github.com/perfect-panel/server/internal/model/user"
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

// NewResetUserSubscribeTokenLogic Reset User Subscribe Token
func NewResetUserSubscribeTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetUserSubscribeTokenLogic {
	return &ResetUserSubscribeTokenLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResetUserSubscribeTokenLogic) ResetUserSubscribeToken(req *types.ResetUserSubscribeTokenRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok || u == nil {
		zap.S().Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	userSub, err := l.svcCtx.Store.User().FindOneUserSubscribe(l.ctx, req.UserSubscribeId)
	if err != nil {
		l.Logger.Errorw("FindOneUserSubscribe failed:", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneUserSubscribe failed: %v", err.Error())
	}
	if userSub == nil || userSub.UserId != u.Id {
		l.Logger.Errorw("UserSubscribeId does not belong to the current user")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "UserSubscribeId does not belong to the current user")
	}

	err = l.svcCtx.Store.User().RotateSubscribeCredentials(l.ctx, userSub.Id, u.Id, uuidx.SubscribeToken(""), uuid.NewString())
	if err != nil {
		l.Logger.Errorw("UpdateSubscribe failed:", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "UpdateSubscribe failed: %v", err.Error())
	}
	// Clear subscription cache
	if err = l.svcCtx.Store.Subscribe().ClearCache(l.ctx, userSub.SubscribeId); err != nil {
		l.Logger.Errorw("ClearSubscribeCache failed", zap.Any("error", err.Error()), zap.Any("subscribeId", userSub.SubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "ClearSubscribeCache failed: %v", err.Error())
	}
	if err = l.svcCtx.Store.Node().ClearServerAllCache(l.ctx); err != nil {
		l.Logger.Errorf("ClearServerAllCache error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "failed to clear server cache: %v", err.Error())
	}
	return nil
}
