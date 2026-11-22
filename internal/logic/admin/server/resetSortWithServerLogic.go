package server

import (
	"context"

	"github.com/perfect-panel/server/internal/logic/admin/listorder"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type ResetSortWithServerLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewResetSortWithServerLogic Reset server sort
func NewResetSortWithServerLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetSortWithServerLogic {
	return &ResetSortWithServerLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResetSortWithServerLogic) ResetSortWithServer(req *types.ResetSortRequest) error {
	return listorder.Servers.Reset(l.ctx, l.svcCtx.Store, req.Sort)
}
