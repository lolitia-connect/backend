package tool

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"go.uber.org/zap"
)

type RestartSystemLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Restart System
func NewRestartSystemLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RestartSystemLogic {
	return &RestartSystemLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RestartSystemLogic) RestartSystem() error {
	l.Logger.Info("[RestartSystem]", zap.Any("info", "Restarting system"))
	go func() {
		err := l.svcCtx.Restart()
		if err != nil {
			l.Logger.Errorw("[RestartSystem]", zap.Any("error", err.Error()))
		}
		l.Logger.Info("[RestartSystem]", zap.Any("info", "System restarted"))
	}()
	return nil
}
