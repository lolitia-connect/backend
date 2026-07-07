package server

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type DeleteNodeLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewDeleteNodeLogic Delete Node
func NewDeleteNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteNodeLogic {
	return &DeleteNodeLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteNodeLogic) DeleteNode(req *types.DeleteNodeRequest) error {
	nodeStore := l.svcCtx.Store.Node()
	data, err := nodeStore.FindOneNode(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorw("[DeleteNode] Query Database Error: ", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "[DeleteNode] Query Database Error")
	}

	err = nodeStore.DeleteNode(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorw("[DeleteNode] Delete Database Error: ", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "[DeleteNode] Delete Database Error")
	}

	return nodeStore.ClearServerCache(l.ctx, data.ServerId)
}
