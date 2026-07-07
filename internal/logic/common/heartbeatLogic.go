package common

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type HeartbeatLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewHeartbeatLogic Heartbeat
func NewHeartbeatLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HeartbeatLogic {
	return &HeartbeatLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *HeartbeatLogic) Heartbeat() (resp *types.HeartbeatResponse, err error) {
	return &types.HeartbeatResponse{
		Status:    true,
		Message:   "service is alive",
		Timestamp: time.Now().Unix(),
	}, nil
}
