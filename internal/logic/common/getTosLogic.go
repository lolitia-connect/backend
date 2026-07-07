package common

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetTosLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get Tos
func NewGetTosLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTosLogic {
	return &GetTosLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetTosLogic) GetTos() (resp *types.GetTosResponse, err error) {
	resp = &types.GetTosResponse{}
	// get Tos config from db
	configs, err := l.svcCtx.Store.System().GetTosConfig(l.ctx)
	if err != nil {
		l.Logger.Errorw("[GetTosLogic] GetTos error: ", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetTos error: %v", err.Error())
	}
	// reflect to response
	tool.SystemConfigSliceReflectToStruct(configs, resp)
	return
}
