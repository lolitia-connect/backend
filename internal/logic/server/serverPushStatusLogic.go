package server

import (
	"context"
	"errors"
	"time"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type ServerPushStatusLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewServerPushStatusLogic Push server status
func NewServerPushStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ServerPushStatusLogic {
	return &ServerPushStatusLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ServerPushStatusLogic) ServerPushStatus(req *types.ServerPushStatusRequest) error {
	// Update node status cache
	err := l.svcCtx.Store.Node().UpdateStatusCache(l.ctx, req.ServerId, &node.Status{
		Cpu:       req.Cpu,
		Mem:       req.Mem,
		Disk:      req.Disk,
		UpdatedAt: req.UpdatedAt,
	})
	if err != nil {
		l.Logger.Errorw("[ServerPushStatus] UpdateNodeStatus error", zap.Any("error", err))
		return errors.New("update node status failed")
	}

	// Update only last_reported_at to avoid optimistic locking conflicts
	// when multiple nodes report status simultaneously.
	now := time.Now().UTC() // Use UTC explicitly to avoid timezone mismatch with PostgreSQL session timezone (#146)
	if err := l.svcCtx.Store.Node().UpdateServerLastReportedAt(l.ctx, req.ServerId, now); err != nil {
		l.Logger.Errorw("[ServerPushStatus] UpdateServerLastReportedAt error", zap.Any("error", err))
	}

	return nil
}
