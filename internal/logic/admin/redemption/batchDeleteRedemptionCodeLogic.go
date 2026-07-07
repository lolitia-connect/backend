package redemption

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type BatchDeleteRedemptionCodeLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Batch delete redemption code
func NewBatchDeleteRedemptionCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchDeleteRedemptionCodeLogic {
	return &BatchDeleteRedemptionCodeLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *BatchDeleteRedemptionCodeLogic) BatchDeleteRedemptionCode(req *types.BatchDeleteRedemptionCodeRequest) error {
	err := l.svcCtx.Store.RedemptionCode().BatchDelete(l.ctx, tool.StringSliceToInt64Slice(req.Ids))
	if err != nil {
		l.Logger.Errorw("[BatchDeleteRedemptionCode] Database Error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "batch delete redemption code error: %v", err.Error())
	}

	return nil
}
