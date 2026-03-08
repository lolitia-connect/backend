package user

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/perfect-panel/server/internal/logic/admin/group"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type CreateUserSubscribeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create user subcribe
func NewCreateUserSubscribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateUserSubscribeLogic {
	return &CreateUserSubscribeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateUserSubscribeLogic) CreateUserSubscribe(req *types.CreateUserSubscribeRequest) error {
	// validate user
	userInfo, err := l.svcCtx.UserModel.FindOne(l.ctx, req.UserId)
	if err != nil {
		l.Errorw("FindOne error", logger.Field("error", err.Error()), logger.Field("userId", req.UserId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOne error: %v", err.Error())
	}
	subs, err := l.svcCtx.UserModel.QueryUserSubscribe(l.ctx, req.UserId)
	if err != nil {
		l.Errorw("QueryUserSubscribe error", logger.Field("error", err.Error()), logger.Field("userId", req.UserId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "QueryUserSubscribe error: %v", err.Error())
	}
	if len(subs) >= 1 && l.svcCtx.Config.Subscribe.SingleModel {
		return errors.Wrapf(xerr.NewErrCode(xerr.SingleSubscribeModeExceedsLimit), "Single subscribe mode exceeds limit")
	}
	sub, err := l.svcCtx.SubscribeModel.FindOne(l.ctx, req.SubscribeId)
	if err != nil {
		l.Errorw("FindOne error", logger.Field("error", err.Error()), logger.Field("subscribeId", req.SubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOne error: %v", err.Error())
	}
	if req.Traffic == 0 {
		req.Traffic = sub.Traffic
	}

	userSub := user.Subscribe{
		UserId:      req.UserId,
		SubscribeId: req.SubscribeId,
		StartTime:   time.Now(),
		ExpireTime:  time.UnixMilli(req.ExpiredAt),
		Traffic:     req.Traffic,
		Download:    0,
		Upload:      0,
		Token:       uuidx.SubscribeToken(fmt.Sprintf("adminCreate:%d", time.Now().UnixMilli())),
		UUID:        uuid.New().String(),
		NodeGroupId: sub.NodeGroupId,
		Status:      1,
	}
	if err = l.svcCtx.UserModel.InsertSubscribe(l.ctx, &userSub); err != nil {
		l.Errorw("InsertSubscribe error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "InsertSubscribe error: %v", err.Error())
	}

	// Trigger user group recalculation (runs in background)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Check if group management is enabled
		var groupEnabled string
		err := l.svcCtx.DB.Table("system").
			Where("`category` = ? AND `key` = ?", "group", "enabled").
			Select("value").
			Scan(&groupEnabled).Error
		if err != nil || groupEnabled != "true" && groupEnabled != "1" {
			l.Debugf("Group management not enabled, skipping recalculation")
			return
		}

		// Get the configured grouping mode
		var groupMode string
		err = l.svcCtx.DB.Table("system").
			Where("`category` = ? AND `key` = ?", "group", "mode").
			Select("value").
			Scan(&groupMode).Error
		if err != nil {
			l.Errorw("Failed to get group mode", logger.Field("error", err.Error()))
			return
		}

		// Validate group mode
		if groupMode != "average" && groupMode != "subscribe" && groupMode != "traffic" {
			l.Debugf("Invalid group mode (current: %s), skipping", groupMode)
			return
		}

		// Trigger group recalculation with the configured mode
		logic := group.NewRecalculateGroupLogic(ctx, l.svcCtx)
		req := &types.RecalculateGroupRequest{
			Mode: groupMode,
		}

		if err := logic.RecalculateGroup(req); err != nil {
			l.Errorw("Failed to recalculate user group",
				logger.Field("user_id", userInfo.Id),
				logger.Field("error", err.Error()),
			)
			return
		}

		l.Infow("Successfully recalculated user group after admin created subscription",
			logger.Field("user_id", userInfo.Id),
			logger.Field("subscribe_id", userSub.Id),
			logger.Field("mode", groupMode),
		)
	}()

	err = l.svcCtx.UserModel.UpdateUserCache(l.ctx, userInfo)
	if err != nil {
		l.Errorw("UpdateUserCache error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "UpdateUserCache error: %v", err.Error())
	}

	err = l.svcCtx.SubscribeModel.ClearCache(l.ctx, userSub.SubscribeId)
	if err != nil {
		logger.Errorw("ClearSubscribe error", logger.Field("error", err.Error()))
	}

	return nil
}
