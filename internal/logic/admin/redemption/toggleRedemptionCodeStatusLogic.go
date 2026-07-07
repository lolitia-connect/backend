package redemption

import (
	"context"
	"github.com/perfect-panel/server/ent"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type ToggleRedemptionCodeStatusLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Toggle redemption code status
func NewToggleRedemptionCodeStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ToggleRedemptionCodeStatusLogic {
	return &ToggleRedemptionCodeStatusLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ToggleRedemptionCodeStatusLogic) ToggleRedemptionCodeStatus(req *types.ToggleRedemptionCodeStatusRequest) error {
	// Find redemption code
	codeInfo, err := l.svcCtx.Store.RedemptionCode().FindOne(l.ctx, req.Id)
	if err != nil {
		if ent.IsNotFound(err) {
			l.Logger.Errorw("[ToggleRedemptionCodeStatus] Redemption code not found", zap.Any("id", req.Id))
			return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "redemption code not found")
		}
		l.Logger.Errorw("[ToggleRedemptionCodeStatus] Database Error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find redemption code error: %v", err.Error())
	}

	// Update status
	codeInfo.Status = req.Status

	err = l.svcCtx.Store.RedemptionCode().Update(l.ctx, codeInfo)
	if err != nil {
		l.Logger.Errorw("[ToggleRedemptionCodeStatus] Database Error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update redemption code status error: %v", err.Error())
	}

	l.Logger.Infow("[ToggleRedemptionCodeStatus] Successfully toggled redemption code status",
		zap.Any("id", req.Id),
		zap.Any("status", req.Status))

	return nil
}
