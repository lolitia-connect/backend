package server

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type DeleteServerLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewDeleteServerLogic Delete Server
func NewDeleteServerLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteServerLogic {
	return &DeleteServerLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteServerLogic) DeleteServer(req *types.DeleteServerRequest) error {
	nodeStore := l.svcCtx.Store.Node()
	if err := nodeStore.DeleteServerWithOverrides(l.ctx, req.Id); err != nil {
		l.Logger.Errorw("[DeleteServer] Delete Server Error: ", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "[DeleteServer] Delete Server Error")
	}
	return nodeStore.ClearServerCache(l.ctx, req.Id)
}
