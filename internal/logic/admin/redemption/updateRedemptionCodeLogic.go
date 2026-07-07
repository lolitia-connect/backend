package redemption

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdateRedemptionCodeLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update redemption code
func NewUpdateRedemptionCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateRedemptionCodeLogic {
	return &UpdateRedemptionCodeLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateRedemptionCodeLogic) UpdateRedemptionCode(req *types.UpdateRedemptionCodeRequest) error {
	redemptionCode, err := l.svcCtx.Store.RedemptionCode().FindOne(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorw("[UpdateRedemptionCode] Find Redemption Code Error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find redemption code error: %v", err.Error())
	}

	// Code is not allowed to be modified
	if req.TotalCount != 0 {
		// Total count cannot be less than used count
		if req.TotalCount < redemptionCode.UsedCount {
			l.Logger.Errorw("[UpdateRedemptionCode] Total count cannot be less than used count",
				zap.Any("total_count", req.TotalCount),
				zap.Any("used_count", redemptionCode.UsedCount))
			return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams),
				"total count cannot be less than used count: total_count=%d, used_count=%d",
				req.TotalCount, redemptionCode.UsedCount)
		}
		redemptionCode.TotalCount = req.TotalCount
	}
	if req.SubscribePlan != 0 {
		redemptionCode.SubscribePlan = req.SubscribePlan
	}
	if req.UnitTime != "" {
		redemptionCode.UnitTime = normalizeUnitTime(req.UnitTime)
	}
	if req.Quantity != 0 {
		redemptionCode.Quantity = req.Quantity
	}

	err = l.svcCtx.Store.RedemptionCode().Update(l.ctx, redemptionCode)
	if err != nil {
		l.Logger.Errorw("[UpdateRedemptionCode] Database Error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update redemption code error: %v", err.Error())
	}

	return nil
}
