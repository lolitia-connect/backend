package server

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
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
	if err := l.svcCtx.Store.Node().SortServersByName(l.ctx); err != nil {
		l.Logger.Errorw("[SortByNameServer] Error: ", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "%v", err)
	}
	return nil
}
