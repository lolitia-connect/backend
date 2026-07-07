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

type GetSubscribeConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetSubscribeConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSubscribeConfigLogic {
	return &GetSubscribeConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetSubscribeConfigLogic) GetSubscribeConfig() (resp *types.SubscribeConfig, err error) {
	resp = &types.SubscribeConfig{}
	// get subscribe config from db
	subscribeConfigs, err := l.svcCtx.Store.System().GetSubscribeConfig(l.ctx)
	if err != nil {
		l.Logger.Errorw("[GetSubscribeConfig] Database query error", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get subscribe config failed: %v", err.Error())
	}

	// reflect to response
	tool.SystemConfigSliceReflectToStruct(subscribeConfigs, resp)
	return resp, nil
}
