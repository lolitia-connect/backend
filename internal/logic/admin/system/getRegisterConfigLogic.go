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

type GetRegisterConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetRegisterConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetRegisterConfigLogic {
	return &GetRegisterConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetRegisterConfigLogic) GetRegisterConfig() (*types.RegisterConfig, error) {
	resp := &types.RegisterConfig{}

	// get register config from database
	configs, err := l.svcCtx.Store.System().GetRegisterConfig(l.ctx)
	if err != nil {
		l.Logger.Errorw("[GetRegisterConfig] Database query error", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get register config error: %v", err.Error())
	}

	// reflect to response
	tool.SystemConfigSliceReflectToStruct(configs, resp)
	return resp, nil
}
