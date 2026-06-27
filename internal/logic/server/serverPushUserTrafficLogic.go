package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	task "github.com/perfect-panel/server/queue/types"
)

//goland:noinspection GoNameStartsWithPackageName
type ServerPushUserTrafficLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewServerPushUserTrafficLogic Push user Traffic
func NewServerPushUserTrafficLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ServerPushUserTrafficLogic {
	return &ServerPushUserTrafficLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ServerPushUserTrafficLogic) ServerPushUserTraffic(req *types.ServerPushUserTrafficRequest) error {
	// Create traffic task
	var request task.TrafficStatistics
	request.ServerId = req.ServerId
	request.Protocol = req.Protocol
	tool.DeepCopy(&request.Logs, req.Traffic)

	// Push traffic task
	val, _ := json.Marshal(request)
	t := asynq.NewTask(task.ForthwithTrafficStatistics, val, asynq.MaxRetry(3))
	info, err := l.svcCtx.Queue.EnqueueContext(l.ctx, t)
	if err != nil {
		l.Errorw("[ServerPushUserTraffic] Push traffic task error", logger.Field("error", err.Error()), logger.Field("task", t))
	} else {
		l.Infow("[ServerPushUserTraffic] Push traffic task success", logger.Field("task", t.Type()), logger.Field("info", string(info.Payload)))
	}

	// Update only last_reported_at to avoid optimistic locking conflicts
	now := time.Now().UTC() // Use UTC explicitly to avoid timezone mismatch with PostgreSQL session timezone (#146)
	if err := l.svcCtx.Store.Node().UpdateServerLastReportedAt(l.ctx, req.ServerId, now); err != nil {
		l.Errorw("[ServerPushUserTraffic] UpdateServerLastReportedAt error", logger.Field("error", err))
	}

	return nil
}
