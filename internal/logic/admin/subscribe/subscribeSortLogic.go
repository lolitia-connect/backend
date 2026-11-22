package subscribe

import (
	"context"

	"github.com/perfect-panel/server/internal/logic/admin/listorder"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type SubscribeSortLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewSubscribeSortLogic Subscribe sort
func NewSubscribeSortLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SubscribeSortLogic {
	return &SubscribeSortLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SubscribeSortLogic) SubscribeSort(req *types.SubscribeSortRequest) error {
	return listorder.Products.Reset(l.ctx, l.svcCtx.Store, req.Sort)
}
