package handler

import (
	"testing"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/svc"
	taskLogic "github.com/perfect-panel/server/queue/logic/task"
	"github.com/perfect-panel/server/queue/types"
)

func TestRegisterHandlersRegistersExchangeRateTask(t *testing.T) {
	mux := asynq.NewServeMux()
	RegisterHandlers(mux, &svc.ServiceContext{})

	h, pattern := mux.Handler(asynq.NewTask(types.SchedulerExchangeRate, nil))
	if h == nil {
		t.Fatalf("no handler registered for %q", types.SchedulerExchangeRate)
	}
	if pattern != types.SchedulerExchangeRate {
		t.Fatalf("handler pattern = %q, want %q", pattern, types.SchedulerExchangeRate)
	}
	if _, ok := h.(*taskLogic.RateLogic); !ok {
		t.Fatalf("handler type = %T, want *task.RateLogic", h)
	}
}
