package server

import (
	"context"

	"github.com/perfect-panel/server/internal/logic/admin/listorder"
	"github.com/perfect-panel/server/internal/svc"
	"go.uber.org/zap"
)

type SortByNameNodeLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSortByNameNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SortByNameNodeLogic {
	return &SortByNameNodeLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SortByNameNodeLogic) SortByNameNode() error {
	return listorder.Nodes.ByName(l.ctx, l.svcCtx.Store)
}
