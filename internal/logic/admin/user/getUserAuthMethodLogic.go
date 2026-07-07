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

type GetUserAuthMethodLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get user auth method
func NewGetUserAuthMethodLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserAuthMethodLogic {
	return &GetUserAuthMethodLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetUserAuthMethodLogic) GetUserAuthMethod(req *types.GetUserAuthMethodRequest) (resp *types.GetUserAuthMethodResponse, err error) {
	methods, err := l.svcCtx.Store.User().FindUserAuthMethods(l.ctx, req.UserId)
	if err != nil {
		l.Logger.Errorw("[GetUserAuthMethodLogic] Get User Auth Method Error:", zap.Any("err", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Get User Auth Method Error")
	}
	list := make([]types.UserAuthMethod, 0)
	tool.DeepCopy(&list, methods)

	return &types.GetUserAuthMethodResponse{
		AuthMethods: list,
	}, nil
}
