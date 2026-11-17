package payment

import (
	"context"
	"encoding/json"
	"strings"

	paymentModel "github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/report"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	exchangeRateAPI "github.com/perfect-panel/server/pkg/exchangeRate"
	"github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/payment/stripe"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdatePaymentMethodLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUpdatePaymentMethodLogic Update Payment Method
func NewUpdatePaymentMethodLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdatePaymentMethodLogic {
	return &UpdatePaymentMethodLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdatePaymentMethodLogic) UpdatePaymentMethod(req *types.UpdatePaymentMethodRequest) (resp *types.PaymentConfig, err error) {
	if payment.ParsePlatform(req.Platform) == payment.UNSUPPORTED {
		l.Logger.Errorw("unsupported payment platform", zap.Any("mark", req.Platform))
		return nil, errors.Wrapf(xerr.NewErrCodeMsg(400, "UNSUPPORTED_PAYMENT_PLATFORM"), "unsupported payment platform: %s", req.Platform)
	}
	paymentStore := l.svcCtx.Store.Payment()
	method, err := paymentStore.FindOne(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorw("find payment method error", zap.Any("id", req.Id), zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find payment method error: %s", err.Error())
	}
	if req.Sort == 0 {
		req.Sort = method.Sort
	}
	if payment.ParsePlatform(req.Platform) != payment.ParsePlatform(method.Platform) {
		return nil, errors.New("payment platform cannot be changed")
	}
	previousPlatform, previousConfig := method.Platform, method.Config
	previousEnabled := method.Enable
	config, err := parsePaymentPlatformConfig(payment.ParsePlatform(req.Platform), req.Config)
	if err != nil {
		return nil, err
	}
	if payment.ParsePlatform(req.Platform) == payment.EPay {
		if err := validateEPayConfig(config, req.Domain); err != nil {
			return nil, err
		}
	}
	tool.DeepCopy(method, req)
	if req.Enable == nil {
		method.Enable = previousEnabled
	}
	method.Config = config
	var previousStripe, nextStripe paymentModel.StripeConfig
	var newWebhook *stripe.Client
	if payment.ParsePlatform(previousPlatform) == payment.Stripe {
		if err := previousStripe.Unmarshal([]byte(previousConfig)); err != nil {
			return nil, err
		}
	}
	if payment.ParsePlatform(method.Platform) == payment.Stripe {
		if err := nextStripe.Unmarshal([]byte(method.Config)); err != nil {
			return nil, err
		}
		if nextStripe.SecretKey == "" {
			nextStripe.SecretKey = previousStripe.SecretKey
		}
		if nextStripe.SecretKey == previousStripe.SecretKey && previousStripe.WebhookSecret != "" {
			nextStripe.WebhookSecret, nextStripe.WebhookID = previousStripe.WebhookSecret, previousStripe.WebhookID
		} else {
			callback, err := payment.NotifyURL(method.Domain, l.svcCtx.Config.Site.Host, "Stripe", method.Token, report.IsGatewayMode())
			if err != nil {
				return nil, err
			}
			newWebhook = stripe.NewClient(stripe.Config{SecretKey: nextStripe.SecretKey})
			endpoint, err := newWebhook.CreateWebhookEndpoint(callback)
			if err != nil {
				return nil, err
			}
			nextStripe.WebhookSecret, nextStripe.WebhookID = endpoint.Secret, endpoint.ID
		}
		encoded, err := nextStripe.Marshal()
		if err != nil {
			return nil, err
		}
		method.Config = string(encoded)
	}

	// Auto-calculate exchange rate if CurrencyUnit changed and differs from system currency
	if req.CurrencyUnit != "" && !strings.EqualFold(req.CurrencyUnit, l.svcCtx.Config.Currency.Unit) {
		if req.ExchangeRate == 0 && l.svcCtx.Config.Currency.AccessKey != "" {
			rate, rateErr := exchangeRateAPI.GetExchangeRete(l.svcCtx.Config.Currency.Unit, strings.ToUpper(req.CurrencyUnit), l.svcCtx.Config.Currency.AccessKey, 1)
			if rateErr != nil {
				l.Logger.Errorw("[UpdatePaymentMethod] auto-calculate exchange rate error", zap.Any("error", rateErr.Error()))
				if method.ExchangeRate == 0 {
					method.ExchangeRate = 1
				}
			} else {
				method.ExchangeRate = rate
			}
		}
	}
	method.CurrencyUnit = strings.ToUpper(method.CurrencyUnit)

	if err := paymentStore.Update(l.ctx, method); err != nil {
		if newWebhook != nil {
			_ = newWebhook.DeleteWebhookEndpoint(context.WithoutCancel(l.ctx), nextStripe.WebhookID)
		}
		l.Logger.Errorw("update payment method error", zap.Any("id", req.Id), zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update payment method error: %s", err.Error())
	}
	if previousStripe.WebhookID != "" && previousStripe.WebhookID != nextStripe.WebhookID {
		client := stripe.NewClient(stripe.Config{SecretKey: previousStripe.SecretKey})
		if err := client.DeleteWebhookEndpoint(context.WithoutCancel(l.ctx), previousStripe.WebhookID); err != nil {
			l.Logger.Warnw("old Stripe webhook cleanup failed", "error", err)
		}
	}
	resp = &types.PaymentConfig{}
	tool.DeepCopy(resp, method)
	var configMap map[string]interface{}
	_ = json.Unmarshal([]byte(method.Config), &configMap)
	resp.Config = configMap
	return
}
