package email

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/model/task"
	"github.com/stretchr/testify/require"
)

func TestWorkerManagerControlsCampaignWorkers(t *testing.T) {
	store := newWorkerTask(t, task.EmailScope{Recipients: []string{"manager@example.com"}}, 0)
	sender := &workerSender{}
	manager := NewWorkerManager(store, sender)
	require.Same(t, manager, NewWorkerManager(store, sender))
	require.Nil(t, manager.GetWorker(999))
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, manager.RunWorker(cancelled, 1, store, sender), context.Canceled)
	require.NoError(t, manager.RunWorker(context.Background(), 1, store, sender))
	require.ElementsMatch(t, []string{"manager@example.com"}, sender.sent)
	require.Nil(t, manager.GetWorker(1))
	manager.RemoveWorker(1)

	active := newWorkerTask(t, task.EmailScope{Recipients: []string{"slow@example.com"}, Interval: 1}, 0)
	slowSender := &workerSender{onSend: func() error { time.Sleep(10 * time.Millisecond); return nil }}
	ctx, stop := context.WithCancel(context.Background())
	started := make(chan error, 1)
	go func() { started <- manager.RunWorker(ctx, 2, active, slowSender) }()
	require.Eventually(t, func() bool { return manager.GetWorker(2) != nil }, time.Second, time.Millisecond)
	manager.RemoveWorker(2)
	stop()
	require.ErrorIs(t, <-started, context.Canceled)
	if err := manager.RunWorker(context.Background(), 2, &workerTaskStore{current: task.Task{Status: 2}}, sender); !errors.Is(err, ErrTaskNotActive) {
		t.Fatalf("inactive worker = %v", err)
	}
}
