package server

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
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
	if err := l.svcCtx.Store.Node().SortNodesByName(l.ctx); err != nil {
		l.Logger.Errorw("[SortByNameNode] Error: ", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "%v", err)
	}
	return nil
}
