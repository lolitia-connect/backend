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

type GetUserSubscribeByIdLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get user subcribe by id
func NewGetUserSubscribeByIdLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserSubscribeByIdLogic {
	return &GetUserSubscribeByIdLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserSubscribeByIdLogic) GetUserSubscribeById(req *types.GetUserSubscribeByIdRequest) (resp *types.UserSubscribeDetail, err error) {
	sub, err := l.svcCtx.Store.User().FindOneUserSubscribe(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorw("[GetUserSubscribeByIdLogic] FindOneUserSubscribe error", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOneUserSubscribe error: %v", err.Error())
	}
	var subscribeDetails types.UserSubscribeDetail
	tool.DeepCopy(&subscribeDetails, sub)
	return &subscribeDetails, nil
}
