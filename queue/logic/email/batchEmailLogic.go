package emailLogic

import (
	"context"
	"strconv"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/email"
	"go.uber.org/zap"
)

type BatchEmailLogic struct {
	svcCtx *svc.ServiceContext
}

type ErrorInfo struct {
	Error string `json:"error"`
	Email string `json:"email"`
	Time  int64  `json:"time"`
}

func NewBatchEmailLogic(svcCtx *svc.ServiceContext) *BatchEmailLogic {
	return &BatchEmailLogic{
		svcCtx: svcCtx,
	}
}

func (l *BatchEmailLogic) ProcessTask(ctx context.Context, task *asynq.Task) error {
	// 解析任务负载
	payload := task.Payload()
	if len(payload) == 0 {
		zap.S().Error("[BatchEmailLogic] ProcessTask failed: empty payload")
		return asynq.SkipRetry
	}
	// 转换获取任务id
	taskID, err := strconv.ParseInt(string(payload), 10, 64)
	if err != nil {
		zap.S().Error("[BatchEmailLogic] ProcessTask failed: invalid task ID",
			zap.Any("error", err.Error()),
			zap.Any("payload", string(payload)),
		)
		return asynq.SkipRetry
	}
	taskInfo, err := l.svcCtx.Store.Task().FindOne(ctx, taskID)
	if err != nil {
		zap.S().Error("[BatchEmailLogic] ProcessTask failed",
			zap.Any("error", err.Error()),
			zap.Any("taskID", taskID),
		)
		return asynq.SkipRetry
	}

	if taskInfo.Status != 0 {
		zap.S().Info("[BatchEmailLogic] ProcessTask skipped: task already processed",
			zap.Any("taskID", taskID),
			zap.Any("status", taskInfo.Status),
		)
		return nil
	}

	sender, err := email.NewSender(l.svcCtx.Config.Email.Platform, l.svcCtx.Config.Email.PlatformConfig, l.svcCtx.Config.Site.SiteName)
	if err != nil {
		zap.S().Error("[BatchEmailLogic] NewSender failed", zap.Any("error", err.Error()))
		return nil
	}
	manager := email.NewWorkerManager(l.svcCtx.Store.Task(), sender)
	if manager == nil {
		zap.S().Error("[BatchEmailLogic] ProcessTask failed: worker manager is nil")
		return asynq.SkipRetry
	}

	// 添加或获取 Worker 实例
	manager.AddWorker(taskID)
	return nil
}
