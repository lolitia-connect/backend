package system

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetNodeMultiplierLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get Node Multiplier
func NewGetNodeMultiplierLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetNodeMultiplierLogic {
	return &GetNodeMultiplierLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetNodeMultiplierLogic) GetNodeMultiplier() (resp *types.GetNodeMultiplierResponse, err error) {
	data, err := l.svcCtx.Store.System().FindNodeMultiplierConfig(l.ctx)
	if err != nil {
		l.Logger.Error("Get Node Multiplier Config Error: ", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Get Node Multiplier Config Error: %s", err.Error())
	}
	var periods []types.TimePeriod
	if data.Value != "" {
		if err := json.Unmarshal([]byte(data.Value), &periods); err != nil {
			l.Logger.Error("Unmarshal Node Multiplier Config Error: ", zap.Any("error", err.Error()), zap.Any("value", data.Value))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Unmarshal Node Multiplier Config Error: %s", err.Error())
		}
	}

	return &types.GetNodeMultiplierResponse{
		Periods: periods,
	}, nil
}
