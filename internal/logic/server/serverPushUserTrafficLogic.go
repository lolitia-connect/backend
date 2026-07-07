package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	task "github.com/perfect-panel/server/queue/types"
	"go.uber.org/zap"
)

//goland:noinspection GoNameStartsWithPackageName
type ServerPushUserTrafficLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewServerPushUserTrafficLogic Push user Traffic
func NewServerPushUserTrafficLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ServerPushUserTrafficLogic {
	return &ServerPushUserTrafficLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ServerPushUserTrafficLogic) ServerPushUserTraffic(req *types.ServerPushUserTrafficRequest) error {
	// Create traffic task
	var request task.TrafficStatistics
	request.ServerId = req.ServerId
	request.Protocol = req.Protocol
	request.ProtocolId = req.ProtocolId
	tool.DeepCopy(&request.Logs, req.Traffic)

	// Push traffic task
	val, _ := json.Marshal(request)
	t := asynq.NewTask(task.ForthwithTrafficStatistics, val, asynq.MaxRetry(3))
	info, err := l.svcCtx.Queue.EnqueueContext(l.ctx, t)
	if err != nil {
		l.Logger.Errorw("[ServerPushUserTraffic] Push traffic task error", zap.Any("error", err.Error()), zap.Any("task", t))
	} else {
		l.Logger.Infow("[ServerPushUserTraffic] Push traffic task success", zap.Any("task", t.Type()), zap.Any("info", string(info.Payload)))
	}

	// Update only last_reported_at to avoid optimistic locking conflicts
	now := time.Now().UTC() // Use UTC explicitly to avoid timezone mismatch with PostgreSQL session timezone (#146)
	if err := l.svcCtx.Store.Node().UpdateServerLastReportedAt(l.ctx, req.ServerId, now); err != nil {
		l.Logger.Errorw("[ServerPushUserTraffic] UpdateServerLastReportedAt error", zap.Any("error", err))
	}

	return nil
}
