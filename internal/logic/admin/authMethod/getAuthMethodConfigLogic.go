package authMethod

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetAuthMethodConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewGetAuthMethodConfigLogic Get auth method config
func NewGetAuthMethodConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAuthMethodConfigLogic {
	return &GetAuthMethodConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetAuthMethodConfigLogic) GetAuthMethodConfig(req *types.GetAuthMethodConfigRequest) (resp *types.AuthMethodConfig, err error) {
	method, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, req.Method)
	if err != nil {
		l.Logger.Errorw("find one by method failed", zap.Any("method", req.Method), zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find one by method failed: %v", err.Error())
	}

	resp = new(types.AuthMethodConfig)
	tool.DeepCopy(resp, method)
	if method.Config != "" {
		if err := json.Unmarshal([]byte(method.Config), &resp.Config); err != nil {
			l.Logger.Errorw("unmarshal config failed", zap.Any("config", method.Config), zap.Any("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal apple config failed: %v", err.Error())
		}
	}
	return
}
