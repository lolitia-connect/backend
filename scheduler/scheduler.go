package scheduler

import (
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logging"
	"github.com/perfect-panel/server/queue/types"
)

type Service struct {
	svc     *svc.ServiceContext
	server  *asynq.Scheduler
	mu      sync.Mutex
	stopped bool
	done    chan struct{}
}

func NewService(svc *svc.ServiceContext) *Service {
	return &Service{
		svc:    svc,
		server: initService(svc),
		done:   make(chan struct{}),
	}
}

func (m *Service) Start() {
	if err := m.Run(); err != nil {
		panic(err)
	}
}

func (m *Service) Run() error {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return nil
	}
	zap.S().Infof("start scheduler service")
	if _, err := m.server.Register("@every 30s", asynq.NewTask(types.SchedulerFlushTraffic, nil), asynq.MaxRetry(3)); err != nil {
		m.mu.Unlock()
		return err
	}
	// schedule check subscription task: every 60 seconds
	checkTask := asynq.NewTask(types.SchedulerCheckSubscription, nil)
	if _, err := m.server.Register("@every 60s", checkTask); err != nil {
		zap.S().Errorf("register check subscription task failed: %s", err.Error())
	}
	//// schedule total server data task: every 5 minutes
	//totalServerDataTask := asynq.NewTask(types.SchedulerTotalServerData, nil)
	//if _, err := m.server.Register("@every 180s", totalServerDataTask); err != nil {
	//	zap.S().Errorf("register total server data task failed: %s", err.Error())
	//}
	// schedule reset traffic task: every day at 00:30
	resetTrafficTask := asynq.NewTask(types.SchedulerResetTraffic, nil)
	if _, err := m.server.Register("30 0 * * *", resetTrafficTask); err != nil {
		zap.S().Errorf("register reset traffic task failed: %s", err.Error())
	}

	// schedule traffic stat task: every day at 00:00
	trafficStatTask := asynq.NewTask(types.SchedulerTrafficStat, nil)
	if _, err := m.server.Register("0 0 * * *", trafficStatTask, asynq.MaxRetry(3)); err != nil {
		zap.S().Errorf("register traffic stat task failed: %s", err.Error())
	}

	// schedule update exchange rate task: every day at 01:00
	rateTask := asynq.NewTask(types.SchedulerExchangeRate, nil)
	if _, err := m.server.Register("0 1 * * *", rateTask, asynq.MaxRetry(3)); err != nil {
		zap.S().Errorf("register update exchange rate task failed: %s", err.Error())
	}

	// schedule recalculate group task: every hour
	recalculateGroupTask := asynq.NewTask(types.SchedulerRecalculateGroup, nil)
	if _, err := m.server.Register("@every 6h", recalculateGroupTask, asynq.MaxRetry(2)); err != nil {
		zap.S().Errorf("register recalculate group task failed: %s", err.Error())
	}

	// schedule order event publisher: every 5 seconds. This is both the retry
	// path for a failed publish and the recovery path for events written while
	// the queue was unavailable, so it runs far more often than any other task.
	publishOrderEventsTask := asynq.NewTask(types.PublishOrderEvents, nil)
	if _, err := m.server.Register("@every 5s", publishOrderEventsTask, asynq.MaxRetry(3)); err != nil {
		zap.S().Errorf("register publish order events task failed: %s", err.Error())
	}

	if _, err := m.server.Register("@every 1m", asynq.NewTask(types.RecoverStripeOrders, nil), asynq.Unique(time.Minute)); err != nil {
		m.mu.Unlock()
		return err
	}

	// schedule order event cleanup: every day at 03:30
	cleanupOrderEventsTask := asynq.NewTask(types.CleanupOrderEvents, nil)
	if _, err := m.server.Register("30 3 * * *", cleanupOrderEventsTask, asynq.MaxRetry(2)); err != nil {
		zap.S().Errorf("register cleanup order events task failed: %s", err.Error())
	}

	err := m.server.Start()
	m.mu.Unlock()
	if err != nil {
		return err
	}
	<-m.done
	return nil
}

func (m *Service) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return
	}
	m.stopped = true
	zap.S().Info("stop scheduler service")
	m.server.Shutdown()
	close(m.done)
}

func initService(svc *svc.ServiceContext) *asynq.Scheduler {
	// Use the process-local timezone so scheduled tasks fire on the same
	// day boundary that the business logic computes with (everything else in
	// the backend uses time.Local). A hardcoded Asia/Shanghai location here
	// made cron tasks fire at Shanghai midnight while the logic derived
	// "yesterday"/"today" from time.Local, so on servers in another timezone
	// the daily traffic archive and the console disagreed by a day.
	return asynq.NewScheduler(
		asynq.RedisClientOpt{Addr: svc.Config.Redis.Host, Password: svc.Config.Redis.Pass, DB: 5},
		&asynq.SchedulerOpts{
			Location: time.Local,
			Logger:   logging.NewAsynqLogger(),
		},
	)
}
