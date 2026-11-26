package task

import (
	"context"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/exchangeRate"
	"github.com/perfect-panel/server/pkg/tool"
	"go.uber.org/zap"
)

type RateLogic struct {
	svcCtx *svc.ServiceContext
	quote  func(from, to, access string, amount float64) (float64, error)
}

func NewRateLogic(svcCtx *svc.ServiceContext) *RateLogic {
	return &RateLogic{svcCtx: svcCtx, quote: exchangeRate.GetExchangeRete}
}

func (l *RateLogic) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	// Retrieve system currency configuration
	currency, err := l.svcCtx.Store.System().GetCurrencyConfig(ctx)
	if err != nil {
		zap.S().Errorw("[PurchaseCheckout] GetCurrencyConfig error", zap.Any("error", err.Error()))
		return err
	}
	// Parse currency configuration
	configs := struct {
		CurrencyUnit   string
		CurrencySymbol string
		AccessKey      string
	}{}
	tool.SystemConfigSliceReflectToStruct(currency, &configs)

	// Skip conversion if no exchange rate API key configured
	if configs.AccessKey == "" {
		zap.S().Debugf("[RateLogic] skip exchange rate, no access key configured")
		return nil
	}
	// Update exchange rates
	result, err := l.quote(configs.CurrencyUnit, "CNY", configs.AccessKey, 1)
	if err != nil {
		zap.S().Errorw("[RateLogic] GetExchangeRete error", zap.Any("error", err.Error()))
		return err
	}
	l.svcCtx.SetCNYRate(configs.CurrencyUnit, result)
	zap.S().Infof("[RateLogic] GetExchangeRete success, result: %+v", result)
	return nil
}
