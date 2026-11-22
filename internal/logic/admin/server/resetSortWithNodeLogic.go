package server

import (
	"context"

	"github.com/perfect-panel/server/internal/logic/admin/listorder"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type ResetSortWithNodeLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewResetSortWithNodeLogic Reset node sort
func NewResetSortWithNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResetSortWithNodeLogic {
	return &ResetSortWithNodeLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResetSortWithNodeLogic) ResetSortWithNode(req *types.ResetSortRequest) error {
	return listorder.Nodes.Reset(l.ctx, l.svcCtx.Store, req.Sort)
}
