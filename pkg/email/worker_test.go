package email

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/model/task"
	"github.com/perfect-panel/server/internal/model/user"
)

type workerTaskStore struct {
	task.Model
	current task.Task
	onSave  func(*task.Task) error
}

func (store *workerTaskStore) FindOneByType(ctx context.Context, id int64, typ task.Type) (*task.Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	copy := store.current
	return &copy, nil
}

func (store *workerTaskStore) SaveEmailProgress(ctx context.Context, current *task.Task) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if store.current.Status != 0 && store.current.Status != 1 {
		return false, nil
	}
	if store.onSave != nil {
		if err := store.onSave(current); err != nil {
			return false, err
		}
	}
	store.current = *current
	return true, nil
}

type workerSender struct {
	sent   []string
	onSend func() error
}

func (sender *workerSender) Send(to []string, subject, body string) error {
	sender.sent = append(sender.sent, to...)
	if sender.onSend != nil {
		return sender.onSend()
	}
	return nil
}

func newWorkerTask(t *testing.T, scope task.EmailScope, current uint64) *workerTaskStore {
	t.Helper()
	encoded, err := scope.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return &workerTaskStore{current: task.Task{
		Id: 1, Type: int8(task.TypeEmail), Scope: string(encoded),
		Content: `{"subject":"subject","content":"body"}`, Current: current,
	}}
}

func TestWorkerResumesRecordedCursorAndFailures(t *testing.T) {
	store := newWorkerTask(t, task.EmailScope{Recipients: []string{"done@example.com", "next@example.com"}}, 1)
	store.current.Status = 1
	store.current.Errors = `[{"email":"old@example.com","error":"old failure","time":1}]`
	sender := &workerSender{onSend: func() error { return errors.New("new failure") }}
	if err := NewWorker(context.Background(), 1, store, sender).Start(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sender.sent, []string{"next@example.com"}) || store.current.Current != 2 || store.current.Status != 2 {
		t.Fatalf("sent=%v, task=%+v", sender.sent, store.current)
	}
	var failures []ErrorInfo
	if err := json.Unmarshal([]byte(store.current.Errors), &failures); err != nil || len(failures) != 2 {
		t.Fatalf("failures=%s, err=%v", store.current.Errors, err)
	}
}

func TestWorkerBudgetPreservesResumePosition(t *testing.T) {
	store := newWorkerTask(t, task.EmailScope{Recipients: []string{"done@example.com", "next@example.com"}}, 1)
	sender := &workerSender{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := NewWorker(ctx, 1, store, sender).Start()
	var budget *RunBudgetExhausted
	if !errors.As(err, &budget) || budget.Sent != 1 || !budget.ResumeAt.After(time.Now()) {
		t.Fatalf("budget=%+v, err=%v", budget, err)
	}
	if len(sender.sent) != 0 || store.current.Current != 1 || store.current.Status != 1 || store.current.Errors != "" {
		t.Fatalf("sent=%v, task=%+v", sender.sent, store.current)
	}
}

func TestWorkerInterruptionDoesNotRecordDeliveryFailure(t *testing.T) {
	store := newWorkerTask(t, task.EmailScope{Recipients: []string{"next@example.com"}}, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sender := &workerSender{onSend: func() error { cancel(); return ctx.Err() }}
	if err := NewWorker(ctx, 1, store, sender).Start(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Start()=%v", err)
	}
	if store.current.Current != 0 || store.current.Errors != "" || store.current.Status != 1 {
		t.Fatalf("interrupted campaign=%+v", store.current)
	}
}

func TestWorkerCannotOverwriteAdministrativeStop(t *testing.T) {
	store := newWorkerTask(t, task.EmailScope{Recipients: []string{"next@example.com"}}, 0)
	sender := &workerSender{onSend: func() error { store.current.Status = 2; return nil }}
	if err := NewWorker(context.Background(), 1, store, sender).Start(); !errors.Is(err, ErrTaskNotActive) {
		t.Fatalf("Start()=%v", err)
	}
	if store.current.Status != 2 || store.current.Current != 0 || store.current.Errors != "" {
		t.Fatalf("stopped campaign=%+v", store.current)
	}
}

type workerRecipientResolver struct {
	selected []string
}

func (resolver workerRecipientResolver) QueryEmailRecipients(context.Context, *user.EmailRecipientFilter) ([]string, error) {
	return resolver.selected, nil
}

func TestWorkerSkipsStaleRecipientsWithoutShiftingCursor(t *testing.T) {
	store := newWorkerTask(t, task.EmailScope{
		Type: task.ScopeAll.Int8(), Recipients: []string{"done@example.com", "deleted@example.com", "manual@example.com"},
		Additional: []string{"manual@example.com"},
	}, 1)
	sender := &workerSender{}
	resolver := workerRecipientResolver{selected: []string{"done@example.com"}}
	if err := NewWorker(context.Background(), 1, store, sender, WithRecipientResolver(resolver)).Start(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sender.sent, []string{"manual@example.com"}) || store.current.Current != 3 || store.current.Total != 3 {
		t.Fatalf("sent=%v, task=%+v", sender.sent, store.current)
	}
}

func TestWorkerStopsOnProgressWriteFailure(t *testing.T) {
	store := newWorkerTask(t, task.EmailScope{Recipients: []string{"first@example.com", "second@example.com"}}, 0)
	writeErr := errors.New("progress storage unavailable")
	store.onSave = func(current *task.Task) error {
		if current.Current > 0 {
			return writeErr
		}
		return nil
	}
	sender := &workerSender{}
	if err := NewWorker(context.Background(), 1, store, sender).Start(); !errors.Is(err, writeErr) {
		t.Fatalf("Start()=%v", err)
	}
	if !reflect.DeepEqual(sender.sent, []string{"first@example.com"}) || store.current.Current != 0 {
		t.Fatalf("sent=%v, task=%+v", sender.sent, store.current)
	}
}

func TestWorkerContinuesAfterDailyLimitWithoutMarkingFailure(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	store := newWorkerTask(t, task.EmailScope{
		Recipients: []string{"first@example.com", "second@example.com"},
		Limit:      1,
	}, 0)
	store.current.Status = 1
	sender := &workerSender{}
	err := NewWorker(context.Background(), 1, store, sender).Start()
	var limit *DailyLimitReached
	if !errors.As(err, &limit) || !limit.NextAt.After(time.Now()) {
		t.Fatalf("limit=%+v, err=%v", limit, err)
	}
	if !reflect.DeepEqual(sender.sent, []string{"first@example.com"}) {
		t.Fatalf("sent=%v", sender.sent)
	}
	if store.current.Current != 1 || store.current.Status != 1 || store.current.DailyDate != today || store.current.DailySent != 1 {
		t.Fatalf("task=%+v", store.current)
	}
	var scope task.EmailScope
	if err := scope.Unmarshal([]byte(store.current.Scope)); err != nil {
		t.Fatal(err)
	}
	if scope.DailyDate != today || scope.DailySent != 1 {
		t.Fatalf("scope=%+v", scope)
	}
}

func TestWorkerResetsDailyLimitAfterDateChanges(t *testing.T) {
	yesterday := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
	store := newWorkerTask(t, task.EmailScope{
		Recipients: []string{"first@example.com"},
		Limit:      1,
		DailyDate:  yesterday,
		DailySent:  1,
	}, 0)
	store.current.Status = 1
	store.current.DailyDate = yesterday
	store.current.DailySent = 1
	sender := &workerSender{}
	if err := NewWorker(context.Background(), 1, store, sender).Start(); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 || store.current.DailyDate == yesterday || store.current.DailySent != 1 || store.current.Status != 2 {
		t.Fatalf("sent=%v, task=%+v", sender.sent, store.current)
	}
}
