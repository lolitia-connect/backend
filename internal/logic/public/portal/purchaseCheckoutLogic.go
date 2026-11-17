package portal

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/exchangeRate"
	"github.com/perfect-panel/server/pkg/requestmeta"
	"github.com/perfect-panel/server/pkg/tasklock"

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
	"github.com/perfect-panel/server/pkg/payment/cryptomus"
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
	lock, held, err := tasklock.Acquire(l.ctx, l.svcCtx.Redis, "order-checkout:"+req.OrderNo, 2*time.Minute)
	if err != nil {
		return nil, err
	}
	if !held {
		return nil, fmt.Errorf("order checkout is already being processed")
	}
	defer lock.Release(l.ctx)
	ctx, cancel := context.WithCancelCause(l.ctx)
	defer cancel(nil)
	stop := lock.KeepAlive(ctx, 2*time.Minute, cancel)
	defer stop()
	l.ctx = ctx

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
	if paymentConfig.Enable == nil || !*paymentConfig.Enable {
		return nil, fmt.Errorf("payment method is disabled")
	}
	if orderInfo.PaymentSession != "" && orderInfo.TradeNo != "" {
		var cached types.CheckoutOrderResponse
		if json.Unmarshal([]byte(orderInfo.PaymentSession), &cached) == nil && cached.CheckoutUrl != "" {
			return &cached, nil
		}
	}
	// Route to appropriate payment handler based on payment platform
	switch paymentPlatform.ParsePlatform(orderInfo.Method) {
	case paymentPlatform.EPay:
		return l.epayPayment(paymentConfig, orderInfo, req.ReturnUrl)

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

	case paymentPlatform.Cryptomus:
		// Process Cryptomus crypto payment - creates a hosted invoice for redirect
		url, err := l.CryptomusPayment(paymentConfig, orderInfo, req.ReturnUrl)
		if err != nil {
			l.Logger.Errorw("[PurchaseCheckout] CryptomusPayment error", zap.Any("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "CryptomusPayment error: %v", err.Error())
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
	if err == nil && resp != nil && resp.CheckoutUrl != "" {
		encoded, encodeErr := json.Marshal(resp)
		if encodeErr != nil {
			return nil, encodeErr
		}
		if orderInfo.TradeNo != "" {
			if err := l.claimPayment(orderInfo, orderInfo.TradeNo, string(encoded)); err != nil {
				return nil, err
			}
		}
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

	notifyUrl, err := l.paymentNotifyURL(pay)
	if err != nil {
		return "", err
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
	convertAmount, err := l.paymentAmount(pay, info, "CNY")
	if err != nil {
		l.Logger.Errorw("[PurchaseCheckout] queryPaymentMethodExchangeRate error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "queryPaymentMethodExchangeRate error: %s", err.Error())
	}

	// Create pre-payment trade and generate QR code
	QRCode, err := client.PreCreateTrade(l.ctx, alipay.Order{
		OrderNo:   info.OrderNo,
		Amount:    convertAmount,
		ExpiresAt: info.CreatedAt.Add(15 * time.Minute),
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

	notifyURL, err := l.paymentNotifyURL(pay)
	if err != nil {
		return "", err
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

	convertAmount, err := l.paymentAmount(pay, info, targetCurrency)
	if err != nil {
		l.Logger.Errorw("[PurchaseCheckout] queryPaymentMethodExchangeRate error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "queryPaymentMethodExchangeRate error: %s", err.Error())
	}

	payload, err := client.PreCreateTrade(l.ctx, alipayplus.Order{
		OrderNo:           info.OrderNo,
		Amount:            convertAmount,
		ReferenceBuyerId:  strconv.FormatInt(info.UserId, 10),
		PaymentExpiryTime: info.CreatedAt.Add(15 * time.Minute).Format(time.RFC3339),
	})
	if err != nil {
		l.Logger.Errorw("[PurchaseCheckout] AlipayPlus PreCreateTrade error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "PreCreateTrade error: %s", err.Error())
	}

	if err := l.claimPayment(info, info.OrderNo, ""); err != nil {
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
	stripeAmount, convertErr := l.paymentAmount(pay, info, stripeCurrency)
	if convertErr != nil {
		l.Logger.Errorw("[PurchaseCheckout] queryPaymentMethodExchangeRate error", zap.Any("error", convertErr.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "queryPaymentMethodExchangeRate error: %s", convertErr.Error())
	}

	// Resolve billing description template
	statementDescriptorSuffix := l.resolveBillDesc(pay.BillDesc, info, "")

	// Build success/cancel URLs
	successURL := returnUrl
	if successURL == "" {
		successURL = strings.TrimSpace(strings.Split(l.svcCtx.Config.Site.Host, "\n")[0])
		if successURL == "" {
			return "", fmt.Errorf("configure the site host for Stripe checkout")
		}
		if !strings.Contains(successURL, "://") {
			successURL = "https://" + successURL
		}
	}
	cancelURL := successURL

	// Create Stripe Checkout Session for redirect-based payment
	result, err := client.CreateCheckoutSessionContext(l.ctx, &stripe.Order{
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
	err = l.claimPayment(info, result.TradeNo, "")
	if err != nil {
		l.Logger.Errorw("[PurchaseCheckout] Update order error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Update error: %s", err.Error())
	}
	return result.CheckoutURL, nil
}

// epayPayment processes EPay payment by generating a payment URL for redirect
// It handles currency conversion and creates a payment URL for external payment processing
func (l *PurchaseCheckoutLogic) epayPayment(config *payment.Payment, info *order.Order, returnUrl string) (*types.CheckoutOrderResponse, error) {
	epayConfig := &payment.EPayConfig{}
	if err := epayConfig.Unmarshal([]byte(config.Config)); err != nil {
		return nil, err
	}
	gateway, err := epay.Open(epay.GatewayConfig{Version: epayConfig.Version, CheckoutMode: epayConfig.CheckoutMode, Endpoint: epayConfig.Url, Domain: config.Domain, MerchantID: epayConfig.Pid, Key: epayConfig.Key, PrivateKey: epayConfig.PrivateKey, PublicKey: epayConfig.PublicKey, PaymentType: epayConfig.Type})
	if err != nil {
		return nil, err
	}
	if info.PaymentSession != "" {
		var saved epay.PaymentSession
		if json.Unmarshal([]byte(info.PaymentSession), &saved) != nil || saved.State != "ready" || saved.TradeNo == "" || saved.TradeNo != info.TradeNo {
			return nil, fmt.Errorf("EPay checkout attempt is awaiting reconciliation")
		}
		kind, entry, err := saved.CheckoutURL()
		if err != nil {
			return nil, err
		}
		return &types.CheckoutOrderResponse{Type: kind, CheckoutUrl: entry}, nil
	}
	minorAmount, err := l.paymentAmount(config, info, "CNY")
	if err != nil {
		return nil, err
	}
	notifyUrl, err := l.paymentNotifyURL(config)
	if err != nil {
		return nil, err
	}

	payName := l.svcCtx.Config.Site.SiteName
	if config.BillDesc != "" {
		resolved := l.resolveBillDesc(config.BillDesc, info, "")
		if resolved != "" {
			payName = resolved
		}
	}

	money := paymentPlatform.FormatAmount(minorAmount)
	if !strings.EqualFold(strings.TrimSpace(epayConfig.CheckoutMode), "api") {
		entry, err := gateway.PagePayment(payName, info.OrderNo, money, notifyUrl, returnUrl)
		if err != nil {
			return nil, err
		}
		return &types.CheckoutOrderResponse{Type: "url", CheckoutUrl: epay.BrowserURL(epayConfig.Url, entry)}, nil
	}
	metadata, _ := requestmeta.From(l.ctx)
	if err := gateway.ValidatePayment(payName, info.OrderNo, money, notifyUrl, returnUrl, metadata.ClientIP); err != nil {
		return nil, err
	}
	const creating = `{"state":"creating"}`
	started, err := l.svcCtx.Store.Order().BeginPaymentSession(l.ctx, info.OrderNo, creating)
	if err != nil {
		return nil, err
	}
	if !started {
		return nil, fmt.Errorf("EPay checkout already started or order is no longer pending")
	}
	session, err := gateway.CreatePayment(l.ctx, payName, info.OrderNo, money, notifyUrl, returnUrl, metadata.ClientIP)
	if err != nil {
		return nil, fmt.Errorf("EPay create outcome requires reconciliation: %w", err)
	}
	session.State = "ready"
	if strings.TrimSpace(session.TradeNo) != session.TradeNo || session.TradeNo == "" || len(session.TradeNo) > 255 {
		return nil, fmt.Errorf("invalid EPay trade number")
	}
	kind, entry, err := session.CheckoutURL()
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(session)
	if err != nil {
		return nil, err
	}
	saved, err := l.svcCtx.Store.Order().CompletePaymentSession(l.ctx, info.OrderNo, creating, session.TradeNo, string(encoded))
	if err != nil {
		return nil, err
	}
	if !saved {
		return nil, fmt.Errorf("EPay order changed before checkout could be saved")
	}
	info.TradeNo, info.PaymentSession = session.TradeNo, string(encoded)
	return &types.CheckoutOrderResponse{Type: kind, CheckoutUrl: entry}, nil
}

// CryptoSaaSPayment processes CryptoSaaSPayment payment by generating a payment URL for redirect
// It handles currency conversion and creates a payment URL for external payment processing
func (l *PurchaseCheckoutLogic) CryptoSaaSPayment(config *payment.Payment, info *order.Order, returnUrl string) (string, error) {
	// Parse EPay configuration from payment settings
	epayConfig := &payment.CryptoSaaSConfig{}
	if err := epayConfig.Unmarshal([]byte(config.Config)); err != nil {
		l.Logger.Errorw("[PurchaseCheckout] Unmarshal EPay config error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Unmarshal error: %s", err.Error())
	}
	// Initialize EPay client with merchant credentials
	client := epay.NewClient(epayConfig.AccountID, epayConfig.Endpoint, epayConfig.SecretKey, epayConfig.Type)

	minorAmount, err := l.paymentAmount(config, info, "CNY")
	if err != nil {
		return "", err
	}
	notifyUrl, err := l.paymentNotifyURL(config)
	if err != nil {
		return "", err
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
	url, err := client.CreatePayUrl(epay.Order{
		Name:        payName,
		AmountMinor: minorAmount,
		OrderNo:     info.OrderNo,
		SignType:    "MD5",
		NotifyUrl:   notifyUrl,
		ReturnUrl:   returnUrl,
	})
	if err != nil {
		l.Logger.Errorw("[PurchaseCheckout] CreatePayUrl error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "CreatePayUrl error: %s", err.Error())
	}
	return url, nil
}

// CryptomusPayment processes a Cryptomus crypto payment by creating a hosted
// invoice in the payment currency; the gateway converts it to the payer's
// chosen cryptocurrency at payment time.
func (l *PurchaseCheckoutLogic) CryptomusPayment(config *payment.Payment, info *order.Order, returnUrl string) (string, error) {
	cryptomusConfig := &payment.CryptomusConfig{}
	if err := cryptomusConfig.Unmarshal([]byte(config.Config)); err != nil {
		l.Logger.Errorw("[PurchaseCheckout] Unmarshal Cryptomus config error", zap.Any("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Unmarshal error: %s", err.Error())
	}
	client := cryptomus.NewClient(cryptomus.Config{
		MerchantID: cryptomusConfig.MerchantID,
		APIKey:     cryptomusConfig.APIKey,
	})

	// Resolve the currency the invoice is denominated in.
	currency := strings.ToUpper(strings.TrimSpace(config.CurrencyUnit))
	if currency == "" {
		currency = strings.ToUpper(l.svcCtx.Config.Currency.Unit)
	}

	// Convert the order amount into minor units of the payment currency.
	amount, err := l.paymentAmount(config, info, currency)
	if err != nil {
		l.Logger.Error("[PurchaseCheckout] queryPaymentMethodExchangeRate error", zap.Any("error", err.Error()))
		return "", err
	}
	if amount <= 0 {
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "invalid Cryptomus payment amount")
	}

	// A pending order owns exactly one Cryptomus invoice. Reuse the one already
	// claimed by the order so checkout retries stay on the same hosted URL.
	if info.TradeNo != "" {
		invoice, lookupErr := client.GetInvoice(info.TradeNo, "")
		if lookupErr == nil && invoice.OrderNo == info.OrderNo && invoice.URL != "" {
			return invoice.URL, nil
		}
	}

	notifyUrl, err := l.paymentNotifyURL(config)
	if err != nil {
		return "", err
	}

	invoice, err := client.CreateInvoice(cryptomus.Order{
		OrderNo:   info.OrderNo,
		Amount:    amount,
		Currency:  currency,
		NotifyURL: notifyUrl,
		ReturnURL: returnUrl,
	})
	if err != nil {
		// The gateway enforces one active invoice per order number. A previous
		// checkout may have created it and crashed before claiming the trade
		// number, so recover that invoice instead of failing the checkout.
		existing, queryErr := client.GetInvoice("", info.OrderNo)
		if queryErr != nil {
			l.Logger.Errorw("[PurchaseCheckout] Create Cryptomus invoice error", zap.Any("error", err.Error()))
			return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "create Cryptomus invoice: %s", err.Error())
		}
		invoice = existing
	}

	if invoice.OrderNo != info.OrderNo {
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Cryptomus invoice order mismatch")
	}
	if invoice.URL == "" {
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Cryptomus invoice has no checkout URL")
	}

	// Claim the invoice on the order for idempotent retries and callbacks.
	if info.TradeNo == "" {
		if err = l.claimPayment(info, invoice.UUID, ""); err != nil {
			l.Logger.Errorw("[PurchaseCheckout] Update order trade no error", zap.Any("error", err.Error()))
			return "", errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Update error: %s", err.Error())
		}
	}
	return invoice.URL, nil
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
	err = l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		locked, err := store.Order().FindOneByOrderNoForUpdate(l.ctx, o.OrderNo)
		if err != nil {
			return err
		}
		if locked.UserId != u.Id || locked.Method != "balance" || locked.Status != order.StatusPending {
			return xerr.NewErrCode(xerr.OrderStatusError)
		}
		if locked.Amount < 0 || locked.GiftAmount < 0 {
			return xerr.NewErrCode(xerr.InvalidParams)
		}
		o = locked
		walletInfo, err := store.Wallet().FindOneForUpdate(l.ctx, u.Id)
		if err != nil {
			return err
		}

		// Check if user has sufficient total balance (regular + gift)
		totalAvailable := walletInfo.Available()
		if totalAvailable < o.Amount {
			return errors.Wrapf(xerr.NewErrCode(xerr.InsufficientBalance),
				"Insufficient balance: required %d, available %d", o.Amount, totalAvailable)
		}

		// Calculate payment distribution: prioritize gift amount first
		giftUsed, balanceUsed := walletInfo.Reserve(o.Amount)

		// Update wallet balances
		walletInfo.GiftAmount -= giftUsed
		walletInfo.Balance -= balanceUsed

		if err = store.Wallet().UpdateBalanceFields(l.ctx, walletInfo); err != nil {
			return err
		}

		// Create gift amount log if gift amount was used
		if giftUsed > 0 {
			giftLog := &log.Gift{
				OrderNo: o.OrderNo,
				Type:    log.GiftTypeReduce, // Type 2 represents gift amount decrease/usage
				Amount:  giftUsed,
				Balance: walletInfo.GiftAmount,
				Remark:  "Purchase payment",
			}
			content, _ := giftLog.Marshal()

			err = store.Log().Insert(l.ctx, &log.SystemLog{
				Type:     log.TypeGift.Uint8(),
				ObjectID: u.Id,
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
				Balance:   walletInfo.Balance,
				Timestamp: time.Now().UnixMilli(),
			}
			content, _ := balanceLog.Marshal()
			err = store.Log().Insert(l.ctx, &log.SystemLog{
				Type:     log.TypeBalance.Uint8(),
				ObjectID: u.Id,
				Date:     time.Now().Format(time.DateOnly),
				Content:  string(content),
			})
			if err != nil {
				return err
			}
		}

		// Store gift amount used in order for potential refund tracking
		o.Amount -= giftUsed
		o.GiftAmount += giftUsed
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
