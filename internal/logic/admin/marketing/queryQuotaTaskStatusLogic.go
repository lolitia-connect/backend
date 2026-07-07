package marketing

import (
	"context"

	"github.com/perfect-panel/server/internal/model/task"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type QueryQuotaTaskStatusLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryQuotaTaskStatusLogic Query quota task status
func NewQueryQuotaTaskStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryQuotaTaskStatusLogic {
	return &QueryQuotaTaskStatusLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryQuotaTaskStatusLogic) QueryQuotaTaskStatus(req *types.QueryQuotaTaskStatusRequest) (resp *types.QueryQuotaTaskStatusResponse, err error) {
	data, err := l.svcCtx.Store.Task().FindOneByType(l.ctx, req.Id, task.TypeQuota)
	if err != nil {
		l.Logger.Errorf("[QueryQuotaTaskStatus] failed to get quota task: %v", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), " failed to get quota task: %v", err.Error())
	}
	return &types.QueryQuotaTaskStatusResponse{
		Status:  uint8(data.Status),
		Current: int64(data.Current),
		Total:   int64(data.Total),
		Errors:  data.Errors,
	}, nil
}
