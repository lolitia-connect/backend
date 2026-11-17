package email

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/perfect-panel/server/internal/model/task"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/pkg/tool"
)

type ErrorInfo struct {
	Error string `json:"error"`
	Email string `json:"email"`
	Time  int64  `json:"time"`
}

var ErrTaskNotActive = errors.New("email campaign is no longer active")

type DailyLimitReached struct {
	NextAt time.Time
}

func (limit *DailyLimitReached) Error() string {
	return fmt.Sprintf("email campaign daily limit reached; resume at %s", limit.NextAt.Format(time.RFC3339))
}

type RunBudgetExhausted struct {
	Sent     uint64
	ResumeAt time.Time
}

func (budget *RunBudgetExhausted) Error() string {
	return fmt.Sprintf("email campaign needs continuation at recipient %d", budget.Sent)
}

type RecipientResolver interface {
	QueryEmailRecipients(context.Context, *user.EmailRecipientFilter) ([]string, error)
}

type WorkerOption func(*Worker)

func WithRecipientResolver(resolver RecipientResolver) WorkerOption {
	return func(worker *Worker) { worker.recipients = resolver }
}

type Worker struct {
	id         int64
	tasks      task.Model
	ctx        context.Context
	sender     Sender
	status     atomic.Uint32
	recipients RecipientResolver
	reserve    time.Duration
}

func NewWorker(ctx context.Context, id int64, tasks task.Model, sender Sender, options ...WorkerOption) *Worker {
	worker := &Worker{id: id, tasks: tasks, ctx: ctx, sender: sender, reserve: time.Minute}
	for _, option := range options {
		if option != nil {
			option(worker)
		}
	}
	return worker
}

func withBudgetReserve(reserve time.Duration) WorkerOption {
	return func(worker *Worker) { worker.reserve = reserve }
}

func (worker *Worker) GetID() int64 {
	return worker.id
}

func (worker *Worker) IsRunning() uint8 {
	return uint8(worker.status.Load())
}

func (worker *Worker) Start() error {
	worker.status.Store(1)
	defer worker.status.Store(2)
	current, err := worker.tasks.FindOneByType(worker.ctx, worker.id, task.TypeEmail)
	if err != nil {
		return err
	}
	if current == nil || (current.Status != 0 && current.Status != 1) {
		return ErrTaskNotActive
	}
	var scope task.EmailScope
	if err := json.Unmarshal([]byte(current.Scope), &scope); err != nil {
		return err
	}
	var content task.EmailContent
	if err := json.Unmarshal([]byte(current.Content), &content); err != nil {
		return err
	}
	recipients := append(append([]string{}, scope.Recipients...), scope.Additional...)
	recipients = tool.RemoveDuplicateElements(recipients...)
	if len(recipients) == 0 || current.Current > uint64(len(recipients)) {
		return errors.New("invalid email campaign recipients or cursor")
	}
	skipped, err := worker.staleRecipients(&scope)
	if err != nil {
		return err
	}
	var failures []ErrorInfo
	if current.Errors != "" {
		if err := json.Unmarshal([]byte(current.Errors), &failures); err != nil {
			return err
		}
	}
	if current.DailyDate == "" && current.DailySent == 0 {
		current.DailyDate = scope.DailyDate
		current.DailySent = scope.DailySent
	} else {
		scope.DailyDate = current.DailyDate
		scope.DailySent = current.DailySent
	}
	today := time.Now().Format("2006-01-02")
	if scope.DailyDate != today {
		scope.DailyDate = today
		scope.DailySent = 0
	}
	current.Total, current.Status = uint64(len(recipients)), task.StatusInProgress
	if err := worker.persist(current, &scope); err != nil {
		return err
	}
	interval := time.Second
	if scope.Interval != 0 {
		interval = time.Duration(scope.Interval) * time.Second
	}
	delivered := false
	for position := int(current.Current); position < len(recipients); position++ {
		if err := worker.ctx.Err(); err != nil {
			return err
		}
		recipient := recipients[position]
		if !skipped[strings.ToLower(strings.TrimSpace(recipient))] {
			if scope.Limit > 0 && scope.DailySent >= scope.Limit {
				now := time.Now()
				next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
				return &DailyLimitReached{NextAt: next}
			}
			if deadline, exists := worker.ctx.Deadline(); exists && time.Until(deadline) < interval+worker.reserve {
				return &RunBudgetExhausted{Sent: current.Current, ResumeAt: time.Now().Add(interval)}
			}
			if delivered {
				timer := time.NewTimer(interval)
				select {
				case <-worker.ctx.Done():
					timer.Stop()
					return worker.ctx.Err()
				case <-timer.C:
				}
			}
			fresh, err := worker.tasks.FindOneByType(worker.ctx, worker.id, task.TypeEmail)
			if err != nil {
				return err
			}
			if fresh == nil || fresh.Status != task.StatusInProgress {
				return ErrTaskNotActive
			}
			if err := SendContext(worker.ctx, worker.sender, []string{recipient}, content.Subject, content.Content); err != nil {
				if worker.ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				failures = append(failures, ErrorInfo{Error: err.Error(), Email: recipient, Time: time.Now().Unix()})
				encoded, err := json.Marshal(failures)
				if err != nil {
					return err
				}
				current.Errors = string(encoded)
			}
			delivered = true
			scope.DailySent++
		}
		current.Current = uint64(position + 1)
		if err := worker.persist(current, &scope); err != nil {
			return err
		}
	}
	current.Status = task.StatusCompleted
	recipientSet := make(map[string]struct{}, len(recipients))
	for _, recipient := range recipients {
		recipientSet[recipient] = struct{}{}
	}
	failedRecipients := make(map[string]struct{}, len(failures))
	for _, failure := range failures {
		if _, ok := recipientSet[failure.Email]; ok {
			failedRecipients[failure.Email] = struct{}{}
		}
	}
	if len(failedRecipients) >= len(recipients) {
		current.Status = task.StatusFailed
	}
	return worker.persist(current, &scope)
}

func (worker *Worker) persist(current *task.Task, scope *task.EmailScope) error {
	if scope != nil {
		encoded, err := scope.Marshal()
		if err != nil {
			return err
		}
		current.Scope = string(encoded)
		current.DailyDate = scope.DailyDate
		current.DailySent = scope.DailySent
	}
	saved, err := worker.tasks.SaveEmailProgress(worker.ctx, current)
	if err != nil {
		return err
	}
	if !saved {
		return ErrTaskNotActive
	}
	return nil
}

func (worker *Worker) staleRecipients(scope *task.EmailScope) (map[string]bool, error) {
	if worker.recipients == nil || len(scope.Recipients) == 0 || task.ParseScopeType(scope.Type) == task.ScopeSkip {
		return nil, nil
	}
	selected, err := worker.recipients.QueryEmailRecipients(worker.ctx, &user.EmailRecipientFilter{
		Scope: scope.Type, RegisterStartTime: scope.RegisterStartTime, RegisterEndTime: scope.RegisterEndTime,
	})
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool)
	for _, recipient := range append(selected, scope.Additional...) {
		allowed[strings.ToLower(strings.TrimSpace(recipient))] = true
	}
	skipped := make(map[string]bool)
	for _, recipient := range scope.Recipients {
		key := strings.ToLower(strings.TrimSpace(recipient))
		if !allowed[key] {
			skipped[key] = true
		}
	}
	return skipped, nil
}
