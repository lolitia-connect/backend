package emailLogic

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/task"
	"github.com/perfect-panel/server/pkg/email"
	queue "github.com/perfect-panel/server/queue/types"
)

type campaignTaskStore struct {
	task.Model
	current *task.Task
}

func (store *campaignTaskStore) FindOneByType(context.Context, int64, task.Type) (*task.Task, error) {
	if store.current == nil {
		return nil, nil
	}
	copy := *store.current
	return &copy, nil
}

type campaignQueueRecorder struct {
	tasks   []*asynq.Task
	options [][]asynq.Option
	err     error
}

func (recorder *campaignQueueRecorder) EnqueueContext(_ context.Context, queued *asynq.Task, options ...asynq.Option) (*asynq.TaskInfo, error) {
	if recorder.err != nil {
		return nil, recorder.err
	}
	recorder.tasks = append(recorder.tasks, queued)
	recorder.options = append(recorder.options, options)
	return &asynq.TaskInfo{}, nil
}

func optionValue(options []asynq.Option, optionType asynq.OptionType) interface{} {
	for _, option := range options {
		if option.Type() == optionType {
			return option.Value()
		}
	}
	return nil
}

func batchTask(id int64) *asynq.Task {
	return asynq.NewTask(queue.ScheduledBatchSendEmail, []byte(strconv.FormatInt(id, 10)))
}

func TestContinueCampaignQueuesBudgetChunk(t *testing.T) {
	resumeAt := time.Now().Add(30 * time.Second)
	store := &campaignTaskStore{}
	recorder := &campaignQueueRecorder{}
	err := continueCampaign(context.Background(), 7, batchTask(7), &email.RunBudgetExhausted{
		Sent: 2, ResumeAt: resumeAt,
	}, store, recorder)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorder.tasks) != 1 || string(recorder.tasks[0].Payload()) != "7" {
		t.Fatalf("queued=%v", recorder.tasks)
	}
	options := recorder.options[0]
	if got := optionValue(options, asynq.TaskIDOpt); got != "marketing-email-7-chunk-2" {
		t.Fatalf("task ID=%v", got)
	}
	if got := optionValue(options, asynq.TimeoutOpt); got != queue.BatchEmailTaskTimeout {
		t.Fatalf("timeout=%v", got)
	}
	if got := optionValue(options, asynq.ProcessAtOpt); got != resumeAt {
		t.Fatalf("process time=%v, want %v", got, resumeAt)
	}
}

func TestContinueCampaignQueuesDailyChunk(t *testing.T) {
	nextAt := time.Date(2026, 11, 15, 0, 0, 0, 0, time.Local)
	recorder := &campaignQueueRecorder{}
	if err := continueCampaign(context.Background(), 7, batchTask(7), &email.DailyLimitReached{NextAt: nextAt}, &campaignTaskStore{}, recorder); err != nil {
		t.Fatal(err)
	}
	if got := optionValue(recorder.options[0], asynq.TaskIDOpt); got != "marketing-email-7-20261115" {
		t.Fatalf("task ID=%v", got)
	}
	if got := optionValue(recorder.options[0], asynq.ProcessAtOpt); got != nextAt {
		t.Fatalf("process time=%v, want %v", got, nextAt)
	}
	if got := optionValue(recorder.options[0], asynq.TimeoutOpt); got != queue.BatchEmailTaskTimeout {
		t.Fatalf("timeout=%v", got)
	}
}

func TestContinueCampaignTreatsDuplicateAsSuccess(t *testing.T) {
	recorder := &campaignQueueRecorder{err: asynq.ErrTaskIDConflict}
	if err := continueCampaign(context.Background(), 7, batchTask(7), &email.RunBudgetExhausted{Sent: 1, ResumeAt: time.Now()}, &campaignTaskStore{}, recorder); err != nil {
		t.Fatal(err)
	}
}

func TestContinueCampaignDoesNotResumeCancelledTask(t *testing.T) {
	store := &campaignTaskStore{current: &task.Task{Id: 7, Type: int8(task.TypeEmail), Status: task.StatusCancelled, Current: 1}}
	recorder := &campaignQueueRecorder{}
	if err := continueCampaign(context.Background(), 7, batchTask(7), context.Canceled, store, recorder); err != nil {
		t.Fatal(err)
	}
	if len(recorder.tasks) != 0 {
		t.Fatalf("queued=%v", recorder.tasks)
	}
}

func TestContinueCampaignResumesActiveInterruptedTask(t *testing.T) {
	store := &campaignTaskStore{current: &task.Task{Id: 7, Type: int8(task.TypeEmail), Status: task.StatusInProgress, Current: 3}}
	recorder := &campaignQueueRecorder{}
	if err := continueCampaign(context.Background(), 7, batchTask(7), errors.New("wrapped: context canceled"), store, recorder); err == nil {
		t.Fatal("an unrelated error must not be treated as an interruption")
	}
	if len(recorder.tasks) != 0 {
		t.Fatalf("queued=%v", recorder.tasks)
	}
	if err := continueCampaign(context.Background(), 7, batchTask(7), context.Canceled, store, recorder); err != nil {
		t.Fatal(err)
	}
	if got := optionValue(recorder.options[0], asynq.TaskIDOpt); got != "marketing-email-7-chunk-3" {
		t.Fatalf("task ID=%v", got)
	}
}
