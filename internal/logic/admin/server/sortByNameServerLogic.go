package server

import (
	"context"

	"github.com/perfect-panel/server/internal/logic/admin/listorder"
	"github.com/perfect-panel/server/internal/svc"
	"go.uber.org/zap"
)

type SortByNameServerLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSortByNameServerLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SortByNameServerLogic {
	return &SortByNameServerLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SortByNameServerLogic) SortByNameServer() error {
	return listorder.Servers.ByName(l.ctx, l.svcCtx.Store)
}
