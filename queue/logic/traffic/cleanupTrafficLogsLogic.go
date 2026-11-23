package traffic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tasklock"
	"github.com/perfect-panel/server/queue/types"
)

type CleanupTrafficLogsLogic struct {
	svc *svc.ServiceContext
}

func NewCleanupTrafficLogsLogic(svc *svc.ServiceContext) *CleanupTrafficLogsLogic {
	return &CleanupTrafficLogsLogic{svc: svc}
}

func enqueueTrafficLogCleanup(ctx context.Context, svc *svc.ServiceContext, end time.Time) error {
	if !svc.Config.Log.AutoClear {
		return nil
	}
	payload, err := json.Marshal(types.TrafficLogCleanup{Cutoff: end.AddDate(0, 0, int(-svc.Config.Log.ClearDays))})
	if err != nil {
		return err
	}
	_, err = svc.Queue.EnqueueContext(ctx, asynq.NewTask(types.CleanupTrafficLogs, payload), asynq.MaxRetry(3), asynq.Unique(24*time.Hour))
	if errors.Is(err, asynq.ErrDuplicateTask) {
		return nil
	}
	return err
}

func (l *CleanupTrafficLogsLogic) ProcessTask(ctx context.Context, task *asynq.Task) error {
	if !l.svc.Config.Log.AutoClear {
		return nil
	}
	var payload types.TrafficLogCleanup
	if err := json.Unmarshal(task.Payload(), &payload); err != nil || payload.Cutoff.IsZero() {
		return fmt.Errorf("invalid traffic cleanup payload: %w", asynq.SkipRetry)
	}
	return withTrafficTaskLock(ctx, l.svc, "task:traffic:cleanup", func(ctx context.Context) error {
		return l.svc.Store.TrafficLog().DeleteBefore(ctx, payload.Cutoff)
	})
}

func withTrafficTaskLock(ctx context.Context, svc *svc.ServiceContext, key string, process func(context.Context) error) error {
	lock, held, err := tasklock.Acquire(ctx, svc.Redis, key, time.Minute)
	if err != nil {
		return err
	}
	if !held {
		return errors.New("traffic task is running on another worker")
	}
	defer lock.Release(ctx)
	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stop := lock.KeepAlive(runCtx, time.Minute, cancel)
	defer stop()
	return process(runCtx)
}
