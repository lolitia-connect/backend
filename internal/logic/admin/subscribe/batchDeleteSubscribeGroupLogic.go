package subscribe

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type BatchDeleteSubscribeGroupLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Batch delete subscribe group
func NewBatchDeleteSubscribeGroupLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchDeleteSubscribeGroupLogic {
	return &BatchDeleteSubscribeGroupLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *BatchDeleteSubscribeGroupLogic) BatchDeleteSubscribeGroup(req *types.BatchDeleteSubscribeGroupRequest) error {
	err := l.svcCtx.Store.Subscribe().BatchDeleteGroup(l.ctx, tool.StringSliceToInt64Slice(req.Ids))
	if err != nil {
		l.Logger.Error("[BatchDeleteSubscribeGroup] Delete Database Error: ", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "batch delete subscribe group failed: %v", err.Error())
	}
	return nil
}
