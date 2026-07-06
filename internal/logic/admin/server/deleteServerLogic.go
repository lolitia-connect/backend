package server

import (
	"context"

	"github.com/perfect-panel/server/ent/serverconfigoverride"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type DeleteServerLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewDeleteServerLogic Delete Server
func NewDeleteServerLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteServerLogic {
	return &DeleteServerLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteServerLogic) DeleteServer(req *types.DeleteServerRequest) error {
	nodeStore := l.svcCtx.Store.Node()
	tx, err := l.svcCtx.Ent.Tx(l.ctx)
	if err != nil {
		l.Errorw("[DeleteServer] Start Transaction Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "[DeleteServer] Delete Server Error")
	}
	if err := func() error {
		if err := tx.Server.DeleteOneID(req.Id).Exec(l.ctx); err != nil {
			return err
		}
		_, err := tx.ServerConfigOverride.Delete().Where(serverconfigoverride.ServerID(req.Id)).Exec(l.ctx)
		return err
	}(); err != nil {
		_ = tx.Rollback()
		l.Errorw("[DeleteServer] Delete Server Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "[DeleteServer] Delete Server Error")
	}
	if err := tx.Commit(); err != nil {
		l.Errorw("[DeleteServer] Commit Transaction Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "[DeleteServer] Delete Server Error")
	}
	return nodeStore.ClearServerCache(l.ctx, req.Id)
}
