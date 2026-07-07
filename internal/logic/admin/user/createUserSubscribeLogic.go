package user

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type CreateUserSubscribeLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create user subcribe
func NewCreateUserSubscribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateUserSubscribeLogic {
	return &CreateUserSubscribeLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateUserSubscribeLogic) CreateUserSubscribe(req *types.CreateUserSubscribeRequest) error {
	// validate user
	subs, err := l.svcCtx.Store.User().QueryUserSubscribe(l.ctx, req.UserId)
	if err != nil {
		l.Logger.Errorw("QueryUserSubscribe error", zap.Any("error", err.Error()), zap.Any("userId", req.UserId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "QueryUserSubscribe error: %v", err.Error())
	}
	if len(subs) >= 1 && l.svcCtx.Config.Subscribe.SingleModel {
		return errors.Wrapf(xerr.NewErrCode(xerr.SingleSubscribeModeExceedsLimit), "Single subscribe mode exceeds limit")
	}
	sub, err := l.svcCtx.Store.Subscribe().FindOne(l.ctx, req.SubscribeId)
	if err != nil {
		l.Logger.Errorw("FindOne error", zap.Any("error", err.Error()), zap.Any("subscribeId", req.SubscribeId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOne error: %v", err.Error())
	}
	userSub := user.Subscribe{
		UserId:           req.UserId,
		SubscribeId:      req.SubscribeId,
		StartTime:        time.Now(),
		ExpireTime:       time.UnixMilli(req.ExpiredAt),
		Traffic:          req.Traffic,
		TrafficUnlimited: req.TrafficUnlimited,
		Download:         0,
		Upload:           0,
		Token:            uuidx.SubscribeToken(fmt.Sprintf("adminCreate:%d", time.Now().UnixMilli())),
		UUID:             uuid.New().String(),
		NodeGroupId:      sub.NodeGroupId,
		Status:           1,
	}
	if err = l.svcCtx.Store.User().InsertSubscribe(l.ctx, &userSub); err != nil {
		l.Logger.Errorw("InsertSubscribe error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "InsertSubscribe error: %v", err.Error())
	}

	err = l.svcCtx.Store.Subscribe().ClearCache(l.ctx, userSub.SubscribeId)
	if err != nil {
		zap.S().Errorw("ClearSubscribe error", zap.Any("error", err.Error()))
	}

	return nil
}
