package system

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetInviteConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetInviteConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetInviteConfigLogic {
	return &GetInviteConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetInviteConfigLogic) GetInviteConfig() (*types.InviteConfig, error) {
	resp := &types.InviteConfig{}
	// get invite config from db
	configs, err := l.svcCtx.Store.System().GetInviteConfig(l.ctx)
	if err != nil {
		l.Logger.Errorw("[GetInviteConfigLogic] get invite config error: ", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get invite config error: %v", err.Error())
	}
	// reflect to response
	tool.SystemConfigSliceReflectToStruct(configs, resp)

	return resp, nil
}
