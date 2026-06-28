package server

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type SortByNameNodeLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSortByNameNodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SortByNameNodeLogic {
	return &SortByNameNodeLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SortByNameNodeLogic) SortByNameNode() error {
	if err := l.svcCtx.Store.Node().SortNodesByName(l.ctx); err != nil {
		l.Errorw("[SortByNameNode] Error: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), err.Error())
	}
	return nil
}
