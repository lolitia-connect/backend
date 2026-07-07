package subscribe

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type DeleteSubscribeGroupLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Delete subscribe group
func NewDeleteSubscribeGroupLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteSubscribeGroupLogic {
	return &DeleteSubscribeGroupLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteSubscribeGroupLogic) DeleteSubscribeGroup(req *types.DeleteSubscribeGroupRequest) error {
	err := l.svcCtx.Store.Subscribe().DeleteGroup(l.ctx, req.Id)
	if err != nil {
		l.Logger.Error("[DeleteSubscribeGroupLogic] delete subscribe group failed: ", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete subscribe group failed: %v", err.Error())
	}
	return nil
}
