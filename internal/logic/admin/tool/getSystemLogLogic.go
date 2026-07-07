package tool

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/pkg/logging"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type GetSystemLogLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewGetSystemLogLogic Get System Log
func NewGetSystemLogLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSystemLogLogic {
	return &GetSystemLogLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetSystemLogLogic) GetSystemLog() (resp *types.LogResponse, err error) {
	lines, err := logging.ReadLastNLines(l.svcCtx.Config.Logger.Path, 50)
	if err != nil {
		l.Logger.Error(err)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "get system log error: %v", err.Error())
	}
	var list []map[string]interface{}
	for _, line := range lines {
		var log map[string]interface{}
		if err = json.Unmarshal([]byte(line), &log); err != nil {
			l.Logger.Error(err)
			continue
		}
		list = append(list, log)
	}

	return &types.LogResponse{
		List: list,
	}, nil
}
