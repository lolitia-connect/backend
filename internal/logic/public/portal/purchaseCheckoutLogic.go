package portal

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/report"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/exchangeRate"

	paymentPlatform "github.com/perfect-panel/server/pkg/payment"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/user"
	queueType "github.com/perfect-panel/server/queue/types"

	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/payment/alipay"
	"github.com/perfect-panel/server/pkg/payment/alipayplus"
	"github.com/perfect-panel/server/pkg/payment/epay"
	"github.com/perfect-panel/server/pkg/payment/stripe"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// PurchaseCheckoutLogic handles the checkout process for various payment methods
// including EPay, Stripe, Alipay F2F, and balance payments
type PurchaseCheckoutLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewPurchaseCheckoutLogic creates a new instance of PurchaseCheckoutLogic
// for handling purchase checkout operations across different payment platforms
func NewPurchaseCheckoutLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PurchaseCheckoutLogic {
	return &PurchaseCheckoutLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// PurchaseCheckout processes the checkout for an order using the specified payment method
// It validates the order, retrieves payment configuration, and routes to the appropriate payment handler
func (l *PurchaseCheckoutLogic) PurchaseCheckout(req *types.CheckoutOrderRequest) (resp *types.CheckoutOrderResponse, err error) {

	// Validate and retrieve order information
	orderInfo, err := l.svcCtx.Store.Order().FindOneByOrderNo(l.ctx, req.OrderNo)
	if err != nil {
		l.Logger.Error("[PurchaseCheckout] Find order failed", zap.Any("error", err.Error()), zap.Any("orderNo", req.OrderNo))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.OrderNotExist), "order not exist: %v", req.OrderNo)
	}

	// Verify order is in pending payment status (status = 1)
	if orderInfo.Status != 1 {
		l.Logger.Error("[PurchaseCheckout] Order status error", zap.Any("status", orderInfo.Status))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.OrderStatusError), "order status error: %v", orderInfo.Status)
	}

	// Retrieve payment method configuration
	paymentConfig, err := l.svcCtx.Store.Payment().FindOne(l.ctx, orderInfo.PaymentId)
	if err != nil {
		l.Logger.Error("[PurchaseCheckout] Database query error", zap.Any("error", err.Error()), zap.Any("payment", orderInfo.Method))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find payment method error: %v", err.Error())
	}
	// Route to appropriate payment handler based on payment platform
	switch paymentPlatform.ParsePlatform(orderInfo.Method) {
	case paymentPlatform.EPay:
		// Process EPay payment - generates payment URL for redirect
		url, err := l.epayPayment(paymentConfig, orderInfo, req.ReturnUrl)
		if err != nil {
			l.Logger.Error("[PurchaseCheckout] epay error", zap.Any("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "epayPayment error: %v", err.Error())
		}
		resp = &types.CheckoutOrderResponse{
			CheckoutUrl: url,
			Type:        "url", // Client should redirect to URL
		}

	case paymentPlatform.Stripe:
		// Process Stripe payment - creates Checkout Session for redirect
		url, err := l.stripePayment(paymentConfig, orderInfo, "", req.ReturnUrl)
		if err != nil {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "stripePayment error: %v", err.Error())
		}
		resp = &types.CheckoutOrderResponse{
			Type:        "url",
			CheckoutUrl: url,
		}

	case paymentPlatform.AlipayF2F:
		// Process Alipay Face-to-Face payment - generates QR code
		url, err := l.alipayF2fPayment(paymentConfig, orderInfo)
		if err != nil {
			l.Logger.Errorw("[PurchaseCheckout] alipayF2fPayment error", zap.Any("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "alipayF2fPayment error: %v", err.Error())
		}
		resp = &types.CheckoutOrderResponse{
			Type:        "qr", // Client should display QR code
			CheckoutUrl: url,
		}

	case paymentPlatform.AlipayPlus:
		url, err := l.alipayPlusPayment(paymentConfig, orderInfo, req.ReturnUrl)
		if err != nil {
			l.Logger.Errorw("[PurchaseCheckout] alipayPlusPayment error", zap.Any("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "alipayPlusPayment error: %v", err.Error())
		}
		resp = &types.CheckoutOrderResponse{
			Type:        "url",
			CheckoutUrl: url,
		}

	case paymentPlatform.CryptoSaaS:
		// Process EPay payment - generates payment URL for redirect
		url, err := l.CryptoSaaSPayment(paymentConfig, orderInfo, req.ReturnUrl)
		if err != nil {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "epayPayment error: %v", err.Error())
		}
		resp = &types.CheckoutOrderResponse{
			CheckoutUrl: url,
			Type:        "url", // Client should redirect to URL
		}

	case paymentPlatform.Balance:
		// Process balance payment - validate user and process payment immediately
		if orderInfo.UserId == 0 {
			l.Logger.Errorw("[PurchaseCheckout] user not found", zap.Any("userId", orderInfo.UserId))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserNotExist), "user not found")
		}

		// Retrieve user information for balance validation
		userInfo, err := l.svcCtx.Store.User().FindOne(l.ctx, orderInfo.UserId)
		if err != nil {
			l.Logger.Errorw("[PurchaseCheckout] FindOne User error", zap.Any("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOne error: %s", err.Error())
		}

		// Process balance payment with gift amount priority logic
		if err = l.balancePayment(userInfo, orderInfo); err != nil {
			return nil, err
		}

		resp = &types.CheckoutOrderResponse{
			Type: "balance", // Payment completed immediately
		}

	default:
		l.Logger.Errorw("[PurchaseCheckout] payment method not found", zap.Any("method", orderInfo.Method))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "payment method not found")
	}
	return
}

// alipayF2fPayment processes Alipay Face-to-Face payment by generating a QR code
// It handles currency conversion and creates a pre-payment trade for QR code scanning
func (l *PurchaseCheckoutLogic) alipayF2fPayment(pay *payment.Payment, info *order.Order) (string, error) {
	// Parse Alipay F2F configuration from payment settings
	f2FConfig := &payment.AlipayF2FConfig{}
	if err := f2FConfig.Unmarshal([]byte(pay.Config)); err != nil {
		l.Logger.Errorw("[PurchaseCheckout] Unmarshal Alipay config error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Unmarshal error: %s", err.Error())
	}

	// Build notification URL for payment status callbacks
	notifyUrl := ""
	if pay.Domain != "" {
		notifyUrl = strings.TrimSuffix(pay.Domain, "/") + "/v1/notify/" + pay.Platform + "/" + pay.Token
	} else {
		host, ok := l.ctx.Value(constant.CtxKeyRequestHost).(string)
		if !ok {
			host = l.svcCtx.Config.Host
		}
		notifyUrl = "https://" + strings.TrimSuffix(host, "/") + "/v1/notify/" + pay.Platform + "/" + pay.Token
	}

	// Initialize Alipay client with configuration
	// Use resolved bill desc if available, otherwise fall back to config's InvoiceName
	invoiceName := f2FConfig.InvoiceName
	if pay.BillDesc != "" {
		resolved := l.resolveBillDesc(pay.BillDesc, info, "")
		if resolved != "" {
			invoiceName = resolved
		}
	}
	client := alipay.NewClient(alipay.Config{
		AppId:       f2FConfig.AppId,
		PrivateKey:  f2FConfig.PrivateKey,
		PublicKey:   f2FConfig.PublicKey,
		InvoiceName: invoiceName,
		NotifyURL:   notifyUrl,
		Sandbox:     f2FConfig.Sandbox,
	})

	// Convert order amount to CNY using current exchange rate
	amount, err := l.queryPaymentMethodExchangeRate(pay, info.Amount, "CNY")
	if err != nil {
		l.Logger.Errorw("[PurchaseCheckout] queryPaymentMethodExchangeRate error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "queryPaymentMethodExchangeRate error: %s", err.Error())
	}
	convertAmount := int64(amount * 100) // Convert to cents for API

	// Create pre-payment trade and generate QR code
	QRCode, err := client.PreCreateTrade(l.ctx, alipay.Order{
		OrderNo: info.OrderNo,
		Amount:  convertAmount,
	})
	if err != nil {
		l.Logger.Errorw("[PurchaseCheckout] PreCreateTrade error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "PreCreateTrade error: %s", err.Error())
	}
	return QRCode, nil
}

func (l *PurchaseCheckoutLogic) alipayPlusPayment(pay *payment.Payment, info *order.Order, returnURL string) (string, error) {
	config := &payment.AlipayPlusConfig{}
	if err := config.Unmarshal([]byte(pay.Config)); err != nil {
		l.Logger.Errorw("[PurchaseCheckout] Unmarshal AlipayPlus config error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Unmarshal error: %s", err.Error())
	}

	targetCurrency := strings.ToUpper(strings.TrimSpace(pay.CurrencyUnit))
	paymentMethod := strings.ToUpper(strings.TrimSpace(config.PaymentMethod))
	if targetCurrency == "" {
		l.Logger.Errorw("[PurchaseCheckout] AlipayPlus currency is empty")
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "AlipayPlus currency is empty")
	}
	if paymentMethod == "" {
		l.Logger.Errorw("[PurchaseCheckout] AlipayPlus payment method is empty")
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "AlipayPlus payment method is empty")
	}

	// Build notification URL for payment status callbacks
	isGatewayMod := report.IsGatewayMode()
	notifyURL := ""
	if pay.Domain != "" {
		notifyURL = pay.Domain
		if isGatewayMod {
			notifyURL += "/api"
		}
		notifyURL += "/v1/notify/" + pay.Platform + "/" + pay.Token
	} else {
		host, ok := l.ctx.Value(constant.CtxKeyRequestHost).(string)
		if !ok {
			host = l.svcCtx.Config.Host
		}
		notifyURL = "https://" + host
		if isGatewayMod {
			notifyURL += "/api"
		}
		notifyURL += "/v1/notify/" + pay.Platform + "/" + pay.Token
	}

	// Use resolved bill desc as invoice name
	invoiceName := l.resolveBillDesc(pay.BillDesc, info, "")

	client := alipayplus.NewClient(alipayplus.Config{
		ClientId:        config.ClientId,
		MerchantId:      config.MerchantId,
		PrivateKey:      config.PrivateKey,
		AlipayPublicKey: config.AlipayPublicKey,
		GatewayUrl:      config.GatewayUrl,
		Currency:        targetCurrency,
		PaymentMethod:   paymentMethod,
		InvoiceName:     invoiceName,
		NotifyURL:       notifyURL,
		RedirectURL:     returnURL,
	})

	amount, err := l.queryPaymentMethodExchangeRate(pay, info.Amount, targetCurrency)
	if err != nil {
		l.Logger.Errorw("[PurchaseCheckout] queryPaymentMethodExchangeRate error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "queryPaymentMethodExchangeRate error: %s", err.Error())
	}
	convertAmount := int64(amount * 100)

	payload, err := client.PreCreateTrade(l.ctx, alipayplus.Order{
		OrderNo:          info.OrderNo,
		Amount:           convertAmount,
		ReferenceBuyerId: strconv.FormatInt(info.UserId, 10),
	})
	if err != nil {
		l.Logger.Errorw("[PurchaseCheckout] AlipayPlus PreCreateTrade error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "PreCreateTrade error: %s", err.Error())
	}

	info.TradeNo = info.OrderNo
	if err := l.svcCtx.Store.Order().Update(l.ctx, info); err != nil {
		l.Logger.Errorw("[PurchaseCheckout] Update order error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Update error: %s", err.Error())
	}

	return payload, nil
}

// stripePayment processes Stripe payment by creating a Checkout Session
// It redirects the user to Stripe's hosted payment page
func (l *PurchaseCheckoutLogic) stripePayment(pay *payment.Payment, info *order.Order, identifier string, returnUrl string) (string, error) {
	// Parse Stripe configuration from payment settings
	stripeConfig := &payment.StripeConfig{}

	if err := stripeConfig.Unmarshal([]byte(pay.Config)); err != nil {
		l.Logger.Errorw("[PurchaseCheckout] Unmarshal Stripe config error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Unmarshal error: %s", err.Error())
	}

	// Initialize Stripe client with API credentials
	client := stripe.NewClient(stripe.Config{
		SecretKey:     stripeConfig.SecretKey,
		PublicKey:     stripeConfig.PublicKey,
		WebhookSecret: stripeConfig.WebhookSecret,
	})

	// Determine Stripe currency: use payment method's CurrencyUnit if set, otherwise fall back to system currency
	stripeCurrency := strings.ToLower(l.svcCtx.Config.Currency.Unit)
	if pay.CurrencyUnit != "" {
		stripeCurrency = strings.ToLower(pay.CurrencyUnit)
	}

	// Apply exchange rate conversion
	convertedAmount, convertErr := l.queryPaymentMethodExchangeRate(pay, info.Amount, "")
	if convertErr != nil {
		l.Logger.Errorw("[PurchaseCheckout] queryPaymentMethodExchangeRate error", zap.Any("error", convertErr.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "queryPaymentMethodExchangeRate error: %s", convertErr.Error())
	}
	// Convert back to cents for Stripe API
	stripeAmount := int64(convertedAmount * 100)

	// Resolve billing description template
	statementDescriptorSuffix := l.resolveBillDesc(pay.BillDesc, info, "")

	// Build success/cancel URLs
	successURL := returnUrl
	if successURL == "" {
		host, ok := l.ctx.Value(constant.CtxKeyRequestHost).(string)
		if !ok {
			host = l.svcCtx.Config.Host
		}
		successURL = "https://" + host
	}
	cancelURL := successURL

	// Create Stripe Checkout Session for redirect-based payment
	result, err := client.CreateCheckoutSession(&stripe.Order{
		OrderNo:                   info.OrderNo,
		Subscribe:                 strconv.FormatInt(info.SubscribeId, 10),
		Amount:                    stripeAmount,
		Currency:                  stripeCurrency,
		Payment:                   stripeConfig.Payment,
		StatementDescriptorSuffix: statementDescriptorSuffix,
	},
		&stripe.User{
			UserId: info.UserId,
			Email:  identifier,
		},
		successURL,
		cancelURL,
	)
	if err != nil {
		l.Logger.Errorw("[PurchaseCheckout] CreateCheckoutSession error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "CreateCheckoutSession error: %s", err.Error())
	}

	// Save Stripe trade number to order for tracking
	info.TradeNo = result.TradeNo
	err = l.svcCtx.Store.Order().Update(l.ctx, info)
	if err != nil {
		l.Logger.Errorw("[PurchaseCheckout] Update order error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Update error: %s", err.Error())
	}
	return result.CheckoutURL, nil
}

// epayPayment processes EPay payment by generating a payment URL for redirect
// It handles currency conversion and creates a payment URL for external payment processing
func (l *PurchaseCheckoutLogic) epayPayment(config *payment.Payment, info *order.Order, returnUrl string) (string, error) {
	var err error
	// Parse EPay configuration from payment settings
	epayConfig := &payment.EPayConfig{}
	if err := epayConfig.Unmarshal([]byte(config.Config)); err != nil {
		l.Logger.Errorw("[PurchaseCheckout] Unmarshal EPay config error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Unmarshal error: %s", err.Error())
	}
	// Initialize EPay client with merchant credentials
	client := epay.NewClient(epayConfig.Pid, epayConfig.Url, epayConfig.Key, epayConfig.Type)
	var amount float64
	if config.CurrencyUnit != "" && !strings.EqualFold(config.CurrencyUnit, l.svcCtx.Config.Currency.Unit) {
		// Use per-payment exchange rate
		converted, convertErr := l.queryPaymentMethodExchangeRate(config, info.Amount, "CNY")
		if convertErr != nil {
			l.Logger.Error("[PurchaseCheckout] queryPaymentMethodExchangeRate error", zap.Any("error", convertErr.Error()))
			return "", convertErr
		}
		amount = converted
	} else if l.svcCtx.Config.Currency.Unit != "CNY" {
		// Convert order amount to CNY using current exchange rate
		amount, err = l.queryExchangeRate("CNY", info.Amount)
		if err != nil {
			l.Logger.Error("[PurchaseCheckout] queryExchangeRate error", zap.Any("error", err.Error()))
			return "", err
		}
	} else {
		amount = float64(info.Amount) / float64(100)
	}

	// gateway mod
	isGatewayMod := report.IsGatewayMode()

	// Build notification URL for payment status callbacks
	notifyUrl := ""
	if config.Domain != "" {
		notifyUrl = strings.TrimSuffix(config.Domain, "/")
		if isGatewayMod {
			notifyUrl += "/api/"
		}
		notifyUrl = strings.TrimSuffix(notifyUrl, "/") + "/v1/notify/" + config.Platform + "/" + config.Token
	} else {
		host, ok := l.ctx.Value(constant.CtxKeyRequestHost).(string)
		if !ok {
			host = l.svcCtx.Config.Host
		}
		notifyUrl = "https://" + strings.TrimSuffix(host, "/")
		if isGatewayMod {
			notifyUrl += "/api"
		}
		notifyUrl = strings.TrimSuffix(notifyUrl, "/") + "/v1/notify/" + config.Platform + "/" + config.Token
	}

	// Use resolved bill desc if available, otherwise fall back to site name
	payName := l.svcCtx.Config.Site.SiteName
	if config.BillDesc != "" {
		resolved := l.resolveBillDesc(config.BillDesc, info, "")
		if resolved != "" {
			payName = resolved
		}
	}

	// Create payment URL for user redirection
	url := client.CreatePayUrl(epay.Order{
		Name:      payName,
		Amount:    amount,
		OrderNo:   info.OrderNo,
		SignType:  "MD5",
		NotifyUrl: notifyUrl,
		ReturnUrl: returnUrl,
	})
	return url, nil
}

// CryptoSaaSPayment processes CryptoSaaSPayment payment by generating a payment URL for redirect
// It handles currency conversion and creates a payment URL for external payment processing
func (l *PurchaseCheckoutLogic) CryptoSaaSPayment(config *payment.Payment, info *order.Order, returnUrl string) (string, error) {
	var err error
	// Parse EPay configuration from payment settings
	epayConfig := &payment.CryptoSaaSConfig{}
	if err := epayConfig.Unmarshal([]byte(config.Config)); err != nil {
		l.Logger.Errorw("[PurchaseCheckout] Unmarshal EPay config error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Unmarshal error: %s", err.Error())
	}
	// Initialize EPay client with merchant credentials
	client := epay.NewClient(epayConfig.AccountID, epayConfig.Endpoint, epayConfig.SecretKey, epayConfig.Type)

	var amount float64

	if config.CurrencyUnit != "" && !strings.EqualFold(config.CurrencyUnit, l.svcCtx.Config.Currency.Unit) {
		// Use per-payment exchange rate
		converted, convertErr := l.queryPaymentMethodExchangeRate(config, info.Amount, "CNY")
		if convertErr != nil {
			return "", convertErr
		}
		amount = converted
	} else if l.svcCtx.Config.Currency.Unit != "CNY" {
		// Convert order amount to CNY using current exchange rate
		amount, err = l.queryExchangeRate("CNY", info.Amount)
		if err != nil {
			return "", err
		}
	} else {
		amount = float64(info.Amount) / float64(100)
	}

	// gateway mod
	isGatewayMod := report.IsGatewayMode()

	// Build notification URL for payment status callbacks
	notifyUrl := ""
	if config.Domain != "" {
		notifyUrl = strings.TrimSuffix(config.Domain, "/")
		if isGatewayMod {
			notifyUrl += "/api/"
		}
		notifyUrl = strings.TrimSuffix(notifyUrl, "/") + "/v1/notify/" + config.Platform + "/" + config.Token
	} else {
		host, ok := l.ctx.Value(constant.CtxKeyRequestHost).(string)
		if !ok {
			host = l.svcCtx.Config.Host
		}

		notifyUrl = "https://" + strings.TrimSuffix(host, "/")
		if isGatewayMod {
			notifyUrl += "/api"
		}
		notifyUrl = strings.TrimSuffix(notifyUrl, "/") + "/v1/notify/" + config.Platform + "/" + config.Token
	}

	// Use resolved bill desc if available, otherwise fall back to site name
	payName := l.svcCtx.Config.Site.SiteName
	if config.BillDesc != "" {
		resolved := l.resolveBillDesc(config.BillDesc, info, "")
		if resolved != "" {
			payName = resolved
		}
	}

	// Create payment URL for user redirection
	url := client.CreatePayUrl(epay.Order{
		Name:      payName,
		Amount:    amount,
		OrderNo:   info.OrderNo,
		SignType:  "MD5",
		NotifyUrl: notifyUrl,
		ReturnUrl: returnUrl,
	})
	return url, nil
}

// queryExchangeRate converts the order amount from system currency to target currency
// It retrieves the current exchange rate and performs currency conversion if needed
func (l *PurchaseCheckoutLogic) queryExchangeRate(to string, src int64) (amount float64, err error) {
	// Convert cents to decimal amount
	amount = float64(src) / float64(100)

	// No conversion needed if target currency matches system currency
	if strings.EqualFold(to, l.svcCtx.Config.Currency.Unit) {
		return amount, nil
	}

	if l.svcCtx.ExchangeRate != 0 && to == "CNY" {
		amount = amount * l.svcCtx.ExchangeRate
		return amount, nil
	}

	// Skip conversion if no exchange rate API key configured
	if l.svcCtx.Config.Currency.AccessKey == "" {
		return amount, nil
	}

	// Convert currency if system currency differs from target currency
	result, err := exchangeRate.GetExchangeRete(l.svcCtx.Config.Currency.Unit, to, l.svcCtx.Config.Currency.AccessKey, 1)
	if err != nil {
		l.Logger.Error("[PurchaseCheckout] QueryExchangeRate error", zap.Any("error", err.Error()))
		return 0, err
	}
	l.svcCtx.ExchangeRate = result
	return result * amount, nil
}

// queryPaymentMethodExchangeRate converts the order amount using the payment method's currency settings.
// If the payment method has a CurrencyUnit and ExchangeRate set, those are used directly.
// Otherwise, falls back to the global queryExchangeRate with the given target currency.
func (l *PurchaseCheckoutLogic) queryPaymentMethodExchangeRate(pay *payment.Payment, src int64, fallbackTo string) (amount float64, err error) {
	// Convert cents to decimal amount
	amount = float64(src) / float64(100)

	// Use per-payment currency settings if available
	if pay.CurrencyUnit != "" {
		// Use the per-payment exchange rate if set, regardless of currency match
		if pay.ExchangeRate > 0 && pay.ExchangeRate != 1 {
			return amount * pay.ExchangeRate, nil
		}
		// No conversion needed if target currency matches system currency
		if strings.EqualFold(pay.CurrencyUnit, l.svcCtx.Config.Currency.Unit) {
			return amount, nil
		}
		// Try to auto-fetch exchange rate
		if l.svcCtx.Config.Currency.AccessKey != "" {
			result, fetchErr := exchangeRate.GetExchangeRete(l.svcCtx.Config.Currency.Unit, strings.ToUpper(pay.CurrencyUnit), l.svcCtx.Config.Currency.AccessKey, 1)
			if fetchErr != nil {
				l.Logger.Error("[PurchaseCheckout] queryPaymentMethodExchangeRate auto-fetch error", zap.Any("error", fetchErr.Error()))
				return amount, nil // Fallback to 1:1
			}
			return result * amount, nil
		}
		return amount, nil // Fallback to 1:1
	}

	// Fall back to the global exchange rate query
	return l.queryExchangeRate(fallbackTo, src)
}

// resolveBillDesc resolves the billing description template with order variables.
// Supported variables: {order_no}, {item_name}, {amount}, {trade_no}
func (l *PurchaseCheckoutLogic) resolveBillDesc(template string, info *order.Order, tradeNo string) string {
	if template == "" {
		return ""
	}

	itemName := ""
	if info.SubscribeId > 0 {
		sub, err := l.svcCtx.Store.Subscribe().FindOne(l.ctx, info.SubscribeId)
		if err == nil && sub != nil {
			itemName = sub.Name
		}
	}

	amountStr := fmt.Sprintf("%.2f", float64(info.Amount)/100)

	result := template
	result = strings.ReplaceAll(result, "{order_no}", info.OrderNo)
	result = strings.ReplaceAll(result, "{item_name}", itemName)
	result = strings.ReplaceAll(result, "{amount}", amountStr)
	result = strings.ReplaceAll(result, "{trade_no}", tradeNo)

	return result
}

// balancePayment processes balance payment with gift amount priority logic
// It prioritizes using gift amount first, then regular balance, and creates proper audit logs
func (l *PurchaseCheckoutLogic) balancePayment(u *user.User, o *order.Order) error {
	var err error
	if o.Amount == 0 {
		// No payment required for zero-amount orders
		l.Logger.Info(
			"[PurchaseCheckout] No payment required for zero-amount order",
			zap.Any("orderNo", o.OrderNo),
			zap.Any("userId", u.Id),
		)
		err = l.svcCtx.Store.Order().UpdateOrderStatus(l.ctx, o.OrderNo, 2)
		if err != nil {
			l.Logger.Errorw("[PurchaseCheckout] Update order status error",
				zap.Any("error", err.Error()),
				zap.Any("orderNo", o.OrderNo),
				zap.Any("userId", u.Id))
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Update order status error: %s", err.Error())
		}
		goto activation
	}

	err = l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		// Retrieve latest user information inside the transaction.
		userInfo, err := store.User().FindOne(l.ctx, u.Id)
		if err != nil {
			return err
		}

		// Check if user has sufficient total balance (regular + gift)
		totalAvailable := userInfo.Balance + userInfo.GiftAmount
		if totalAvailable < o.Amount {
			return errors.Wrapf(xerr.NewErrCode(xerr.InsufficientBalance),
				"Insufficient balance: required %d, available %d", o.Amount, totalAvailable)
		}

		// Calculate payment distribution: prioritize gift amount first
		var giftUsed, balanceUsed int64
		remainingAmount := o.Amount

		if userInfo.GiftAmount >= remainingAmount {
			// Gift amount covers the entire payment
			giftUsed = remainingAmount
			balanceUsed = 0
		} else {
			// Use all available gift amount, then regular balance
			giftUsed = userInfo.GiftAmount
			balanceUsed = remainingAmount - giftUsed
		}

		// Update user balances
		userInfo.GiftAmount -= giftUsed
		userInfo.Balance -= balanceUsed

		// Save updated user information
		err = store.User().Update(l.ctx, userInfo)
		if err != nil {
			return err
		}

		// Create gift amount log if gift amount was used
		if giftUsed > 0 {
			giftLog := &log.Gift{
				OrderNo: o.OrderNo,
				Type:    log.GiftTypeReduce, // Type 2 represents gift amount decrease/usage
				Amount:  giftUsed,
				Balance: userInfo.GiftAmount,
				Remark:  "Purchase payment",
			}
			content, _ := giftLog.Marshal()

			err = store.Log().Insert(l.ctx, &log.SystemLog{
				Type:     log.TypeGift.Uint8(),
				ObjectID: userInfo.Id,
				Date:     time.Now().Format(time.DateOnly),
				Content:  string(content),
			})
			if err != nil {
				return err
			}
		}

		// Create balance log if regular balance was used
		if balanceUsed > 0 {
			balanceLog := &log.Balance{
				Amount:    balanceUsed,
				Type:      log.BalanceTypePayment, // Type 3 represents payment deduction
				OrderNo:   o.OrderNo,
				Balance:   userInfo.Balance,
				Timestamp: time.Now().UnixMilli(),
			}
			content, _ := balanceLog.Marshal()
			err = store.Log().Insert(l.ctx, &log.SystemLog{
				Type:     log.TypeBalance.Uint8(),
				ObjectID: userInfo.Id,
				Date:     time.Now().Format(time.DateOnly),
				Content:  string(content),
			})
			if err != nil {
				return err
			}
		}

		// Store gift amount used in order for potential refund tracking
		o.GiftAmount = giftUsed
		err = store.Order().Update(l.ctx, o)
		if err != nil {
			return err
		}

		// Mark order as paid (status = 2)
		return store.Order().UpdateOrderStatus(l.ctx, o.OrderNo, 2)
	})

	if err != nil {
		l.Logger.Errorw("[PurchaseCheckout] Balance payment transaction error",
			zap.Any("error", err.Error()),
			zap.Any("orderNo", o.OrderNo),
			zap.Any("userId", u.Id))
		return err
	}

activation:
	// Enqueue order activation task for immediate processing
	payload := queueType.ForthwithActivateOrderPayload{
		OrderNo: o.OrderNo,
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		l.Logger.Errorw("[PurchaseCheckout] Marshal activation payload error", zap.Any("error", err.Error()))
		return err
	}

	task := asynq.NewTask(queueType.ForthwithActivateOrder, bytes, asynq.MaxRetry(5))
	_, err = l.svcCtx.Queue.EnqueueContext(l.ctx, task)
	if err != nil {
		l.Logger.Errorw("[PurchaseCheckout] Enqueue activation task error", zap.Any("error", err.Error()))
		return err
	}

	l.Logger.Info("[PurchaseCheckout] Balance payment completed successfully",
		zap.Any("orderNo", o.OrderNo),
		zap.Any("userId", u.Id))
	return nil
}
