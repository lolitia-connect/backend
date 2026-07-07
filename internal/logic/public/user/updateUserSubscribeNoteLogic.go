package user

import (
	"context"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdateUserSubscribeNoteLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUpdateUserSubscribeNoteLogic Update User Subscribe Note
func NewUpdateUserSubscribeNoteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserSubscribeNoteLogic {
	return &UpdateUserSubscribeNoteLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateUserSubscribeNoteLogic) UpdateUserSubscribeNote(req *types.UpdateUserSubscribeNoteRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	userSub, err := l.svcCtx.Store.User().FindOneUserSubscribe(l.ctx, req.UserSubscribeId)
	if err != nil {
		l.Logger.Errorw("FindOneUserSubscribe failed:", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneUserSubscribe failed: %v", err.Error())
	}

	if userSub.UserId != u.Id {
		l.Logger.Errorw("UserSubscribeId does not belong to the current user")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "UserSubscribeId does not belong to the current user")
	}

	userSub.Note = req.Note
	var newSub user.Subscribe
	tool.DeepCopy(&newSub, userSub)

	err = l.svcCtx.Store.User().UpdateSubscribe(l.ctx, &newSub)
	if err != nil {
		l.Logger.Errorw("UpdateSubscribe failed:", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "UpdateSubscribe failed: %v", err.Error())
	}

	// Clear subscription cache
	if err = l.svcCtx.Store.Subscribe().ClearCache(l.ctx, userSub.SubscribeId); err != nil {
		l.Logger.Errorw("ClearSubscribeCache failed", zap.Any("error", err.Error()), zap.Any("subscribeId", userSub.SubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "ClearSubscribeCache failed: %v", err.Error())
	}

	return nil
}
