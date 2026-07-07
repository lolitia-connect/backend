package coupon

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type BatchDeleteCouponLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Batch delete coupon
func NewBatchDeleteCouponLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchDeleteCouponLogic {
	return &BatchDeleteCouponLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *BatchDeleteCouponLogic) BatchDeleteCoupon(req *types.BatchDeleteCouponRequest) error {
	// batch delete coupon by ids
	err := l.svcCtx.Store.Coupon().BatchDelete(l.ctx, tool.StringSliceToInt64Slice(req.Ids))
	if err != nil {
		l.Logger.Errorw("[BatchDeleteCoupon] Database Error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "batch delete coupon error: %v", err.Error())
	}
	return nil
}
