package marketing

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"go.uber.org/zap"
)

type GetBatchSendEmailTaskStatusLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewGetBatchSendEmailTaskStatusLogic Get batch send email task status
func NewGetBatchSendEmailTaskStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetBatchSendEmailTaskStatusLogic {
	return &GetBatchSendEmailTaskStatusLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetBatchSendEmailTaskStatusLogic) GetBatchSendEmailTaskStatus(req *types.GetBatchSendEmailTaskStatusRequest) (resp *types.GetBatchSendEmailTaskStatusResponse, err error) {
	taskInfo, err := l.svcCtx.Store.Task().FindOne(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorf("failed to get email task status, error: %v", err)
		return nil, xerr.NewErrCode(xerr.DatabaseQueryError)
	}

	return &types.GetBatchSendEmailTaskStatusResponse{
		Status:  uint8(taskInfo.Status),
		Total:   int64(taskInfo.Total),
		Current: int64(taskInfo.Current),
		Errors:  taskInfo.Errors,
	}, nil
}
