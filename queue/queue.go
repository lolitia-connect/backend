package queue

import (
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logging"
	"github.com/perfect-panel/server/queue/handler"
	"go.uber.org/zap"
	"sync"
)

type Service struct {
	svc     *svc.ServiceContext
	server  *asynq.Server
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
	zap.S().Infof("start consumer service")
	mux := asynq.NewServeMux()
	// register tasks
	handler.RegisterHandlers(mux, m.svc)
	err := m.server.Start(mux)
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
	zap.S().Info("stop consumer service")
	m.server.Shutdown()
	close(m.done)
}

func initService(svc *svc.ServiceContext) *asynq.Server {
	return asynq.NewServer(
		asynq.RedisClientOpt{Addr: svc.Config.Redis.Host, Password: svc.Config.Redis.Pass, DB: 5},
		asynq.Config{
			Logger: logging.NewAsynqLogger(),
			IsFailure: func(err error) bool {
				zap.S().Error("consumer service error", zap.Any("error", err.Error()))
				return true
			},
			Concurrency: 20,
		},
	)
}
