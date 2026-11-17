package node

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestUserListBuildQueueCapacityAndCancellation(t *testing.T) {
	active, waiting := make(chan struct{}, 1), make(chan struct{}, 1)
	active <- struct{}{}
	waiting <- struct{}{}
	if err := acquireUserListBuild(context.Background(), active, waiting); !errors.Is(err, ErrServerBusy) {
		t.Fatalf("full queue = %v", err)
	}
	<-waiting
	ctx, cancel := context.WithCancel(context.Background())
	completed := make(chan error, 1)
	go func() { completed <- acquireUserListBuild(ctx, active, waiting) }()
	cancel()
	if err := <-completed; !errors.Is(err, context.Canceled) || !errors.Is(err, ErrServerBusy) {
		t.Fatalf("canceled queue = %v", err)
	}
	if len(waiting) != 0 || len(active) != 1 {
		t.Fatal("canceled queue leaked a slot")
	}
	<-active
	if err := acquireUserListBuild(context.Background(), active, waiting); err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatal("released build slot was not reused")
	}
}

func TestSharedUserIndexSurvivesCallerCancellation(t *testing.T) {
	cache := NewUserIndexCache()
	started, release := make(chan struct{}), make(chan struct{})
	var loads atomic.Int64
	var generation atomic.Int64
	version := func(context.Context) (string, error) {
		if generation.Load() == 0 {
			return "0", nil
		}
		return "1", nil
	}
	load := func(ctx context.Context) (*UserIndex, error) {
		if loads.Add(1) == 1 {
			close(started)
			<-release
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		current, _ := version(ctx)
		return &UserIndex{Generation: current, ExpiresAt: time.Now().Add(time.Minute), IDs: map[int64]struct{}{7: {}}, Limits: map[int64]int64{7: 1}}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { _, err := cache.Get(ctx, "scope", version, load); finished <- err }()
	<-started
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter = %v", err)
	}
	close(release)
	index, err := cache.Get(context.Background(), "scope", version, load)
	if err != nil || index.Generation != "0" || loads.Load() != 1 {
		t.Fatalf("shared load = %+v, %v, loads=%d", index, err, loads.Load())
	}
	generation.Store(1)
	index, err = cache.Get(context.Background(), "scope", version, load)
	if err != nil || index.Generation != "1" || loads.Load() != 2 {
		t.Fatalf("invalidated load = %+v, %v, loads=%d", index, err, loads.Load())
	}
}
