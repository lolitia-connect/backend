package marketing

import (
	"context"

	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/task"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/email"
	"github.com/perfect-panel/server/pkg/xerr"
	"go.uber.org/zap"
)

type StopBatchSendEmailTaskLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewStopBatchSendEmailTaskLogic Stop a batch send email task
func NewStopBatchSendEmailTaskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StopBatchSendEmailTaskLogic {
	return &StopBatchSendEmailTaskLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *StopBatchSendEmailTaskLogic) StopBatchSendEmailTask(req *types.StopBatchSendEmailTaskRequest) (err error) {
	err = l.svcCtx.Store.Task().UpdateStatus(l.ctx, req.Id, task.StatusCancelled)

	if err != nil {
		l.Logger.Errorf("failed to stop email task, error: %v", err)
		return xerr.NewErrCode(xerr.DatabaseUpdateError)
	}
	if email.Manager != nil {
		email.Manager.RemoveWorker(req.Id)
	}
	modellog.RecordAdminAction(l.ctx, l.svcCtx.Store.Log(), modellog.AdminAction{Action: "marketing.email.stop", Object: "task", ObjectID: req.Id})
	return
}
