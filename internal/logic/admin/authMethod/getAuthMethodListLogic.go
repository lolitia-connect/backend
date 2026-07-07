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

type GetAuthMethodListLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewGetAuthMethodListLogic Get auth method list
func NewGetAuthMethodListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAuthMethodListLogic {
	return &GetAuthMethodListLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetAuthMethodListLogic) GetAuthMethodList() (resp *types.GetAuthMethodListResponse, err error) {
	methods, err := l.svcCtx.Store.Auth().FindAll(l.ctx)
	if err != nil {
		l.Logger.Errorw("find all failed", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find all failed: %v", err.Error())
	}
	var list []types.AuthMethodConfig
	for _, method := range methods {
		var item types.AuthMethodConfig
		tool.DeepCopy(&item, method)
		if method.Config != "" {
			if err := json.Unmarshal([]byte(method.Config), &item.Config); err != nil {
				l.Logger.Errorw("unmarshal config failed", zap.Any("config", method.Config), zap.Any("error", err.Error()))
				return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal config failed: %v", err.Error())
			}
		}
		list = append(list, item)
	}
	return &types.GetAuthMethodListResponse{List: list}, nil
}
