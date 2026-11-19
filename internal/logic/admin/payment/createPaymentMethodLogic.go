package payment

import (
	"context"
	"encoding/json"
	"strings"

	paymentModel "github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/report"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	exchangeRateAPI "github.com/perfect-panel/server/pkg/exchangeRate"
	"github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/payment/epay"
	"github.com/perfect-panel/server/pkg/payment/stripe"
	"github.com/perfect-panel/server/pkg/random"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type CreatePaymentMethodLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewCreatePaymentMethodLogic Create Payment Method
func NewCreatePaymentMethodLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreatePaymentMethodLogic {
	return &CreatePaymentMethodLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreatePaymentMethodLogic) CreatePaymentMethod(req *types.CreatePaymentMethodRequest) (resp *types.PaymentConfig, err error) {
	if payment.ParsePlatform(req.Platform) == payment.UNSUPPORTED {
		l.Logger.Errorw("unsupported payment platform", zap.Any("mark", req.Platform))
		return nil, errors.Wrapf(xerr.NewErrCodeMsg(400, "UNSUPPORTED_PAYMENT_PLATFORM"), "unsupported payment platform: %s", req.Platform)
	}
	config, err := parsePaymentPlatformConfig(payment.ParsePlatform(req.Platform), req.Config)
	if err != nil {
		return nil, err
	}
	if payment.ParsePlatform(req.Platform) == payment.EPay {
		if err := validateEPayConfig(config, req.Domain); err != nil {
			return nil, err
		}
	}

	// Auto-calculate exchange rate if CurrencyUnit is set and differs from system currency
	exchangeRate := req.ExchangeRate
	if req.CurrencyUnit != "" && !strings.EqualFold(req.CurrencyUnit, l.svcCtx.Config.Currency.Unit) {
		if exchangeRate == 0 && l.svcCtx.Config.Currency.AccessKey != "" {
			rate, err := exchangeRateAPI.GetExchangeRete(l.svcCtx.Config.Currency.Unit, strings.ToUpper(req.CurrencyUnit), l.svcCtx.Config.Currency.AccessKey, 1)
			if err != nil {
				l.Logger.Errorw("[CreatePaymentMethod] auto-calculate exchange rate error", zap.Any("error", err.Error()))
				// Fall back to provided rate or 1:1
				if req.ExchangeRate == 0 {
					exchangeRate = 1
				}
			} else {
				exchangeRate = rate
			}
		}
	}

	var paymentMethod = &paymentModel.Payment{
		Name:         req.Name,
		Platform:     req.Platform,
		Icon:         req.Icon,
		Domain:       req.Domain,
		Description:  req.Description,
		Config:       config,
		FeeMode:      req.FeeMode,
		FeePercent:   req.FeePercent,
		FeeAmount:    req.FeeAmount,
		Sort:         req.Sort,
		Enable:       req.Enable,
		Token:        random.KeyNew(8, 1),
		CurrencyUnit: strings.ToUpper(req.CurrencyUnit),
		ExchangeRate: exchangeRate,
		BillDesc:     req.BillDesc,
	}
	var createdWebhookID string
	var webhookClient *stripe.Client
	err = l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		if payment.ParsePlatform(req.Platform) == payment.Stripe {
			var cfg paymentModel.StripeConfig
			if err = cfg.Unmarshal([]byte(paymentMethod.Config)); err != nil {
				l.Logger.Errorf("[CreatePaymentMethod] unmarshal stripe config error: %s", err.Error())
				return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal stripe config error: %s", err.Error())
			}
			if cfg.SecretKey == "" {
				l.Logger.Error("[CreatePaymentMethod] stripe secret key is empty")
				return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "stripe secret key is empty")
			}

			// Create Stripe webhook endpoint
			client := stripe.NewClient(stripe.Config{
				SecretKey: cfg.SecretKey,
				PublicKey: cfg.PublicKey,
			})
			url, err := payment.NotifyURL(req.Domain, l.svcCtx.Config.Site.Host, "Stripe", paymentMethod.Token, report.IsGatewayMode())
			if err != nil {
				return err
			}
			endpoint, err := client.CreateWebhookEndpoint(url)
			if err != nil {
				l.Logger.Errorw("[CreatePaymentMethod] create stripe webhook endpoint error", zap.Any("error", err.Error()))
				return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "create stripe webhook endpoint error: %s", err.Error())
			}
			cfg.WebhookSecret = endpoint.Secret
			cfg.WebhookID = endpoint.ID
			createdWebhookID, webhookClient = endpoint.ID, client
			content, _ := cfg.Marshal()
			paymentMethod.Config = string(content)
		}
		if err = store.Payment().Insert(l.ctx, paymentMethod); err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert payment method error: %s", err.Error())
		}
		return nil
	})
	if err != nil {
		if createdWebhookID != "" {
			_ = webhookClient.DeleteWebhookEndpoint(context.WithoutCancel(l.ctx), createdWebhookID)
		}
		return nil, err
	}

	resp = &types.PaymentConfig{}
	tool.DeepCopy(resp, paymentMethod)
	var configMap map[string]interface{}
	_ = json.Unmarshal([]byte(paymentMethod.Config), &configMap)
	resp.Config = configMap
	return
}

func parsePaymentPlatformConfig(platform payment.Platform, config interface{}) (string, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return "", errors.New("invalid payment configuration")
	}

	// 通用处理函数
	handleConfig := func(name string, target interface {
		Unmarshal([]byte) error
		Marshal() ([]byte, error)
	}) (string, error) {
		if err = target.Unmarshal(data); err != nil {
			return "", errors.New("invalid " + name + " configuration")
		}
		content, err := target.Marshal()
		if err != nil {
			return "", errors.New("invalid " + name + " configuration")
		}
		return string(content), nil
	}

	switch platform {
	case payment.Stripe:
		return handleConfig("Stripe", &paymentModel.StripeConfig{})
	case payment.AlipayF2F:
		return handleConfig("Alipay", &paymentModel.AlipayF2FConfig{})
	case payment.AlipayPlus:
		return handleConfig("AlipayPlus", &paymentModel.AlipayPlusConfig{})
	case payment.EPay:
		return handleConfig("Epay", &paymentModel.EPayConfig{})
	case payment.CryptoSaaS:
		return handleConfig("CryptoSaaS", &paymentModel.CryptoSaaSConfig{})
	case payment.Cryptomus:
		return handleConfig("Cryptomus", &paymentModel.CryptomusConfig{})
	default:
		return "", errors.New("unsupported payment platform")
	}
}

func validateEPayConfig(encoded, domain string) error {
	var config paymentModel.EPayConfig
	if err := config.Unmarshal([]byte(encoded)); err != nil {
		return errors.New("invalid EPay configuration")
	}
	_, err := epay.Open(epay.GatewayConfig{Version: config.Version, CheckoutMode: config.CheckoutMode, Endpoint: config.Url, Domain: domain, MerchantID: config.Pid, Key: config.Key, PrivateKey: config.PrivateKey, PublicKey: config.PublicKey, PaymentType: config.Type})
	return err
}
