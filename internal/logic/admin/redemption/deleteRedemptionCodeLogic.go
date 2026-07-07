package redemption

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type DeleteRedemptionCodeLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Delete redemption code
func NewDeleteRedemptionCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteRedemptionCodeLogic {
	return &DeleteRedemptionCodeLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteRedemptionCodeLogic) DeleteRedemptionCode(req *types.DeleteRedemptionCodeRequest) error {
	err := l.svcCtx.Store.RedemptionCode().Delete(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorw("[DeleteRedemptionCode] Database Error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete redemption code error: %v", err.Error())
	}

	return nil
}
