package queue

import (
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/queue/handler"
	"go.uber.org/zap"
)

type Service struct {
	svc    *svc.ServiceContext
	server *asynq.Server
}

func NewService(svc *svc.ServiceContext) *Service {
	return &Service{
		svc:    svc,
		server: initService(svc),
	}
}

func (m *Service) Start() {
	zap.S().Infof("start consumer service")
	mux := asynq.NewServeMux()
	// register tasks
	handler.RegisterHandlers(mux, m.svc)
	if err := m.server.Run(mux); err != nil {
		zap.L().Error("consumer service error", zap.Any("error", err.Error()))
	}
}

func (m *Service) Stop() {
	zap.S().Info("stop consumer service")
	m.server.Stop()
}

func initService(svc *svc.ServiceContext) *asynq.Server {
	return asynq.NewServer(
		asynq.RedisClientOpt{Addr: svc.Config.Redis.Host, Password: svc.Config.Redis.Pass, DB: 5},
		asynq.Config{
			IsFailure: func(err error) bool {
				zap.S().Error("consumer service error", zap.Any("error", err.Error()))
				return true
			},
			Concurrency: 20,
		},
	)
}
