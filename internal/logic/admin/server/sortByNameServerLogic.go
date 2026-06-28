package server

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type SortByNameServerLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSortByNameServerLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SortByNameServerLogic {
	return &SortByNameServerLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SortByNameServerLogic) SortByNameServer() error {
	if err := l.svcCtx.Store.Node().SortServersByName(l.ctx); err != nil {
		l.Errorw("[SortByNameServer] Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), err.Error())
	}
	return nil
}
