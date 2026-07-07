package payment

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type DeletePaymentMethodLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Delete Payment Method
func NewDeletePaymentMethodLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeletePaymentMethodLogic {
	return &DeletePaymentMethodLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeletePaymentMethodLogic) DeletePaymentMethod(req *types.DeletePaymentMethodRequest) error {
	if err := l.svcCtx.Store.Payment().Delete(l.ctx, req.Id); err != nil {
		l.Logger.Errorw("delete payment method error", zap.Any("id", req.Id), zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete payment method error: %s", err.Error())
	}
	return nil
}
