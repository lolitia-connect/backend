package system

import (
	"context"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetVerifyConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetVerifyConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetVerifyConfigLogic {
	return &GetVerifyConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetVerifyConfigLogic) GetVerifyConfig() (*types.VerifyConfig, error) {
	resp := &types.VerifyConfig{}
	// get verify config from db
	verifyConfigs, err := l.svcCtx.Store.System().GetVerifyConfig(l.ctx)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get verify config failed: %v", err.Error())
	}
	// reflect to response
	tool.SystemConfigSliceReflectToStruct(verifyConfigs, resp)
	// update verify config to system
	initialize.Verify(l.svcCtx)
	return resp, nil
}
