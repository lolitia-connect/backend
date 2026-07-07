package system

import (
	"context"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type UpdateCurrencyConfigLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update Currency Config
func NewUpdateCurrencyConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateCurrencyConfigLogic {
	return &UpdateCurrencyConfigLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateCurrencyConfigLogic) UpdateCurrencyConfig(req *types.CurrencyConfig) error {
	err := updateConfigFields(l.ctx, l.svcCtx, "currency", convertedConfigFields(*req), config.CurrencyConfigKey, config.GlobalConfigKey)
	initialize.Currency(l.svcCtx)
	if err != nil {
		l.Logger.Errorw("[UpdateCurrencyConfig] update currency config error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update invite config error: %v", err)
	}
	return nil
}
