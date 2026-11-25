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

	active := newWorkerTask(t, task.EmailScope{Recipients: []string{"slow@example.com"}}, 0)
	sending, release := make(chan struct{}), make(chan struct{})
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	slowSender := &workerSender{onSend: func() error {
		close(sending)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	done := make(chan error, 1)
	go func() { done <- manager.RunWorker(ctx, 2, active, slowSender) }()
	select {
	case <-sending:
	case err := <-done:
		t.Fatalf("worker stopped before sending: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not reach the sender")
	}
	require.NotNil(t, manager.GetWorker(2))
	manager.RemoveWorker(2)
	close(release)
	require.ErrorIs(t, <-done, context.Canceled)
	require.Zero(t, active.current.Current)
	require.Nil(t, manager.GetWorker(2))
	if err := manager.RunWorker(context.Background(), 2, &workerTaskStore{current: task.Task{Status: 2}}, sender); !errors.Is(err, ErrTaskNotActive) {
		t.Fatalf("inactive worker = %v", err)
	}
}
