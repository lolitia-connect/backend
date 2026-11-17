package order

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	paymentnotify "github.com/perfect-panel/server/internal/logic/notify"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/payment"
	paymentutil "github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/payment/alipay"
	"github.com/perfect-panel/server/pkg/payment/alipayplus"
	"github.com/perfect-panel/server/pkg/payment/cryptomus"
	"github.com/perfect-panel/server/pkg/payment/epay"
	"github.com/perfect-panel/server/pkg/payment/stripe"
)

var errPaymentRecovered = errors.New("payment recovered during order close")

func (l *CloseOrderLogic) closeGateway(current *order.Order) error {
	platform := paymentutil.ParsePlatform(current.Method)
	if platform == paymentutil.Balance {
		return nil
	}
	if platform == paymentutil.Stripe && current.TradeNo == "" && current.PaymentCurrency == "" {
		return nil
	}
	method, err := l.svcCtx.Store.Payment().FindOne(l.ctx, current.PaymentId)
	if err != nil {
		return err
	}
	if method == nil {
		return fmt.Errorf("payment method not found")
	}
	settle := func(orderNo, tradeNo string, amount int64, currency string) error {
		if orderNo != current.OrderNo {
			return fmt.Errorf("gateway payment belongs to another order")
		}
		if err := paymentnotify.SettleConfirmedPayment(l.ctx, l.svcCtx, method, current.OrderNo, tradeNo, amount, currency); err != nil {
			return err
		}
		return errPaymentRecovered
	}
	switch platform {
	case paymentutil.Stripe:
		if current.TradeNo == "" {
			return fmt.Errorf("Stripe checkout creation has not been confirmed")
		}
		var config payment.StripeConfig
		if err := json.Unmarshal([]byte(method.Config), &config); err != nil {
			return err
		}
		client := stripe.NewClient(stripe.Config{SecretKey: config.SecretKey})
		receipt, err := client.QueryPayment(l.ctx, current.TradeNo)
		if err != nil {
			return err
		}
		if receipt.Paid {
			return settle(receipt.OrderNo, receipt.TradeNo, receipt.Amount, receipt.Currency)
		}
		if err := client.CancelCheckout(l.ctx, current.TradeNo); err != nil {
			receipt, queryErr := client.QueryPayment(l.ctx, current.TradeNo)
			if queryErr == nil && receipt.Paid {
				return settle(receipt.OrderNo, receipt.TradeNo, receipt.Amount, receipt.Currency)
			}
			return err
		}
		return nil
	case paymentutil.AlipayF2F:
		var config payment.AlipayF2FConfig
		if err := config.Unmarshal([]byte(method.Config)); err != nil {
			return err
		}
		client := alipay.NewClient(alipay.Config{AppId: config.AppId, PrivateKey: config.PrivateKey, PublicKey: config.PublicKey, Sandbox: config.Sandbox})
		receipt, err := client.QueryPayment(l.ctx, current.OrderNo)
		if err != nil {
			return err
		}
		if receipt.Status == alipay.Success || receipt.Status == alipay.Finished {
			return settle(receipt.OrderNo, receipt.TradeNo, receipt.Amount, "CNY")
		}
		if err := client.CloseTrade(l.ctx, current.OrderNo); err != nil {
			receipt, queryErr := client.QueryPayment(l.ctx, current.OrderNo)
			if queryErr == nil && (receipt.Status == alipay.Success || receipt.Status == alipay.Finished) {
				return settle(receipt.OrderNo, receipt.TradeNo, receipt.Amount, "CNY")
			}
			return err
		}
		return nil
	case paymentutil.AlipayPlus:
		var config payment.AlipayPlusConfig
		if err := config.Unmarshal([]byte(method.Config)); err != nil {
			return err
		}
		status, err := alipayplus.NewClient(alipayplus.Config{ClientId: config.ClientId, MerchantId: config.MerchantId, PrivateKey: config.PrivateKey, AlipayPublicKey: config.AlipayPublicKey, GatewayUrl: config.GatewayUrl, Currency: method.CurrencyUnit, PaymentMethod: config.PaymentMethod}).QueryTrade(l.ctx, current.OrderNo)
		if err != nil {
			return err
		}
		if status != alipayplus.Closed && status != alipayplus.Failed {
			return fmt.Errorf("AlipayPlus payment is not confirmed canceled")
		}
		return nil
	case paymentutil.Cryptomus:
		var config payment.CryptomusConfig
		if err := config.Unmarshal([]byte(method.Config)); err != nil {
			return err
		}
		invoice, err := cryptomus.NewClient(cryptomus.Config{MerchantID: config.MerchantID, APIKey: config.APIKey}).GetInvoice(current.TradeNo, current.OrderNo)
		if cryptomus.IsNotFound(err) && current.TradeNo == "" && current.PaymentCurrency == "" {
			return nil
		}
		if err != nil {
			return err
		}
		amount, amountErr := cryptomus.ParseMoney(invoice.Amount)
		if amountErr != nil {
			return amountErr
		}
		if invoice.IsFinal && invoice.Paid() {
			return settle(invoice.OrderNo, invoice.UUID, amount, invoice.Currency)
		}
		received, receivedErr := cryptomus.ParseMoney(invoice.PaymentAmount)
		expectedAmount, expectedCurrency := current.PaymentAmount, current.PaymentCurrency
		if expectedCurrency == "" {
			expectedAmount, expectedCurrency = current.Amount, method.CurrencyUnit
		}
		if receivedErr != nil || invoice.OrderNo != current.OrderNo || amount != expectedAmount || !strings.EqualFold(invoice.Currency, expectedCurrency) || (current.TradeNo != "" && invoice.UUID != current.TradeNo) {
			return fmt.Errorf("Cryptomus payment cannot be verified")
		}
		if !invoice.IsFinal || invoice.State() != cryptomus.StatusCancel || received != 0 {
			return fmt.Errorf("Cryptomus invoice is still payable or holds received funds")
		}
		return nil
	case paymentutil.EPay, paymentutil.CryptoSaaS:
		config := payment.EPayConfig{}
		if platform == paymentutil.CryptoSaaS {
			var crypto payment.CryptoSaaSConfig
			if err := crypto.Unmarshal([]byte(method.Config)); err != nil {
				return err
			}
			config.Pid, config.Url, config.Key, config.Type = crypto.AccountID, crypto.Endpoint, crypto.SecretKey, crypto.Type
		} else if err := config.Unmarshal([]byte(method.Config)); err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(config.Version), "v2") {
			gateway, err := epay.Open(epay.GatewayConfig{Version: config.Version, CheckoutMode: config.CheckoutMode, Endpoint: config.Url, Domain: method.Domain, MerchantID: config.Pid, Key: config.Key, PrivateKey: config.PrivateKey, PublicKey: config.PublicKey, PaymentType: config.Type})
			if err != nil {
				return err
			}
			verify := func(receipt *epay.OrderReceipt) (int64, error) {
				if receipt == nil || receipt.Refunded || receipt.StatusOnly {
					return 0, fmt.Errorf("EPay payment needs manual reconciliation")
				}
				amount, err := epay.ParseMoney(receipt.Money)
				expectedAmount, expectedCurrency := current.PaymentAmount, current.PaymentCurrency
				if expectedCurrency == "" {
					expectedAmount, expectedCurrency = current.Amount, "CNY"
				}
				if err != nil || amount != expectedAmount || !strings.EqualFold(expectedCurrency, "CNY") {
					return 0, fmt.Errorf("EPay payment amount does not match order")
				}
				return amount, nil
			}
			receipt, err := gateway.QueryOrder(l.ctx, current.OrderNo, current.TradeNo)
			if err != nil {
				return err
			}
			amount, err := verify(receipt)
			if err != nil {
				return err
			}
			if receipt.Paid {
				return settle(receipt.OrderNo, receipt.TradeNo, amount, "CNY")
			}
			if !receipt.Unpaid {
				return fmt.Errorf("EPay payment is not confirmed unpaid")
			}
			if closeErr := gateway.CloseOrder(l.ctx, current.OrderNo, receipt.TradeNo); closeErr != nil {
				latest, queryErr := gateway.QueryOrder(l.ctx, current.OrderNo, receipt.TradeNo)
				if queryErr == nil && latest.Paid {
					amount, err := verify(latest)
					if err != nil {
						return err
					}
					return settle(latest.OrderNo, latest.TradeNo, amount, "CNY")
				}
				return fmt.Errorf("EPay remote close is unconfirmed: %w", closeErr)
			}
			return nil
		}
		endpoint, err := epay.ResolveEndpoint(config.Url, method.Domain)
		if err != nil {
			return err
		}
		result, err := epay.NewClient(config.Pid, endpoint, config.Key, config.Type).QueryOrder(current.OrderNo)
		if err != nil {
			if l.expiry {
				return fmt.Errorf("cannot expire order without gateway confirmation: %w", err)
			}
			return nil
		}
		if err == nil {
			if result == nil {
				return fmt.Errorf("gateway returned no payment result")
			}
			if !result.StatusOnly && (result.OrderNo != current.OrderNo || result.MerchantID != config.Pid) {
				return fmt.Errorf("EPay payment identity does not match order")
			}
			if result.Paid {
				if result.StatusOnly {
					return fmt.Errorf("EPay status-only response cannot verify collected payment")
				}
				if result.Type == "" || (config.Type != "" && config.Type != result.Type) {
					return fmt.Errorf("EPay payment type does not match order")
				}
				amount, err := epay.ParseMoney(result.Money)
				if err != nil {
					return err
				}
				return settle(result.OrderNo, result.TradeNo, amount, "CNY")
			}
			if l.expiry && !result.Unpaid {
				return fmt.Errorf("gateway did not confirm an unpaid order")
			}
		}
		if l.expiry && time.Since(current.CreatedAt) < 30*time.Minute {
			return fmt.Errorf("EPay unpaid reconciliation window is not over")
		}
		return nil
	default:
		return fmt.Errorf("payment platform cannot be safely closed")
	}
}
