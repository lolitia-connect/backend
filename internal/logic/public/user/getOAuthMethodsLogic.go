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

type GetOAuthMethodsLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get OAuth Methods
func NewGetOAuthMethodsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetOAuthMethodsLogic {
	return &GetOAuthMethodsLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetOAuthMethodsLogic) GetOAuthMethods() (resp *types.GetOAuthMethodsResponse, err error) {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	methods, err := l.svcCtx.Store.User().FindUserAuthMethods(l.ctx, u.Id)
	if err != nil {
		l.Logger.Errorw("find user auth methods failed:", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find user auth methods failed: %v", err.Error())
	}
	list := make([]types.UserAuthMethod, 0)
	tool.DeepCopy(&list, methods)
	return &types.GetOAuthMethodsResponse{
		Methods: list,
	}, nil
}
