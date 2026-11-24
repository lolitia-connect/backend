package group

import (
	"context"
	"errors"
	"time"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tasklock"
)

var errGroupBusy = errors.New("group recalculation or reset is already running")

func withGroupLock(ctx context.Context, svc *svc.ServiceContext, run func(context.Context) error) error {
	const ttl = time.Minute
	lock, held, err := tasklock.Acquire(ctx, svc.Redis, "task:group:mutate", ttl)
	if err != nil {
		return err
	}
	if !held {
		return errGroupBusy
	}
	defer lock.Release(ctx)
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stop := lock.KeepAlive(ctx, ttl, cancel)
	defer stop()
	return run(ctx)
}
