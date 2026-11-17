package emailLogic

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/hibiken/asynq"
	taskmodel "github.com/perfect-panel/server/internal/model/task"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/email"
	"github.com/perfect-panel/server/pkg/tasklock"
	queue "github.com/perfect-panel/server/queue/types"
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
	if err != nil || taskID <= 0 {
		zap.S().Error("[BatchEmailLogic] ProcessTask failed: invalid task ID",
			zap.Any("error", err),
			zap.Any("payload", string(payload)),
		)
		return asynq.SkipRetry
	}
	taskInfo, err := l.svcCtx.Store.Task().FindOneByType(ctx, taskID, taskmodel.TypeEmail)
	if err != nil {
		zap.S().Error("[BatchEmailLogic] ProcessTask failed",
			zap.Any("error", err.Error()),
			zap.Any("taskID", taskID),
		)
		return err
	}

	if taskInfo == nil {
		return asynq.SkipRetry
	}
	if taskInfo.Status != 0 && taskInfo.Status != 1 {
		zap.S().Info("[BatchEmailLogic] ProcessTask skipped: task already processed",
			zap.Any("taskID", taskID),
			zap.Any("status", taskInfo.Status),
		)
		return nil
	}

	sender, err := email.NewSender(l.svcCtx.Config.Email.Platform, l.svcCtx.Config.Email.PlatformConfig, l.svcCtx.Config.Site.SiteName)
	if err != nil {
		zap.S().Error("[BatchEmailLogic] NewSender failed", zap.Any("error", err.Error()))
		return err
	}
	manager := email.NewWorkerManager(l.svcCtx.Store.Task(), sender)
	if manager == nil {
		zap.S().Error("[BatchEmailLogic] ProcessTask failed: worker manager is nil")
		return asynq.SkipRetry
	}

	lock, held, err := tasklock.Acquire(ctx, l.svcCtx.Redis, fmt.Sprintf("task:email:campaign:%d", taskID), time.Minute)
	if err != nil {
		return err
	}
	if !held {
		return errors.New("email campaign is running on another worker")
	}
	defer lock.Release(ctx)
	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stop := lock.KeepAlive(runCtx, time.Minute, cancel)
	defer stop()
	err = manager.RunWorker(runCtx, taskID, l.svcCtx.Store.Task(), sender, email.WithRecipientResolver(l.svcCtx.Store.User()))
	return continueCampaign(ctx, taskInfo.Id, task, err, l.svcCtx.Store.Task(), l.svcCtx.Queue)
}

type campaignQueue interface {
	EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
}

func continueCampaign(ctx context.Context, taskID int64, queued *asynq.Task, cause error, tasks taskmodel.Model, queueClient campaignQueue) error {
	if cause == nil || errors.Is(cause, email.ErrTaskNotActive) {
		return nil
	}
	var budget *email.RunBudgetExhausted
	var dailyLimit *email.DailyLimitReached
	processAt := time.Now()
	var current uint64
	continuationID := ""
	if errors.As(cause, &dailyLimit) {
		processAt = dailyLimit.NextAt
		continuationID = fmt.Sprintf("marketing-email-%d-%s", taskID, dailyLimit.NextAt.Format("20060102"))
	} else if errors.As(cause, &budget) {
		processAt, current = budget.ResumeAt, budget.Sent
		continuationID = fmt.Sprintf("marketing-email-%d-chunk-%d", taskID, current)
	} else if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		if ctx.Err() != nil {
			return cause
		}
		campaign, err := tasks.FindOneByType(ctx, taskID, taskmodel.TypeEmail)
		if err != nil {
			return errors.Join(cause, err)
		}
		if campaign == nil || (campaign.Status != 0 && campaign.Status != 1) {
			return nil
		}
		current = campaign.Current
	} else {
		return cause
	}
	if queueClient == nil {
		return errors.New("email campaign continuation queue is unavailable")
	}
	if continuationID == "" {
		continuationID = fmt.Sprintf("marketing-email-%d-chunk-%d", taskID, current)
	}
	_, err := queueClient.EnqueueContext(ctx, asynq.NewTask(queued.Type(), queued.Payload()),
		asynq.ProcessAt(processAt), asynq.TaskID(continuationID),
		asynq.Timeout(queue.BatchEmailTaskTimeout))
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}
