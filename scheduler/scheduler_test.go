package scheduler

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/queue/types"
	"github.com/stretchr/testify/require"
)

func TestShutdownReturnsFromRun(t *testing.T) {
	redis := miniredis.RunT(t)
	service := NewService(&svc.ServiceContext{Config: config.Config{Redis: config.RedisConfig{Host: redis.Addr()}}})
	result := make(chan error, 1)
	go func() { result <- service.Run() }()
	t.Cleanup(service.Stop)
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: redis.Addr(), DB: 5})
	t.Cleanup(func() { _ = inspector.Close() })
	require.Eventually(t, func() bool {
		jobs, err := inspector.ListPendingTasks("default")
		if err != nil {
			return false
		}
		for _, job := range jobs {
			if job.Type == types.PublishOrderEvents {
				return true
			}
		}
		return false
	}, 20*time.Second, 100*time.Millisecond, "the running scheduler should enqueue order event publication")
	service.Stop()
	service.Stop()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("scheduler still waiting after shutdown")
	}
}
