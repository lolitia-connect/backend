package portal

import (
	"fmt"
	"math"
	"strings"

	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/report"
	"github.com/perfect-panel/server/internal/repository"
	paymentutil "github.com/perfect-panel/server/pkg/payment"
	stripepayment "github.com/perfect-panel/server/pkg/payment/stripe"
)

func paymentNotifyURL(domain, siteHost, platform, token string, gateway bool) (string, error) {
	return paymentutil.NotifyURL(domain, siteHost, platform, token, gateway)
}

func (l *PurchaseCheckoutLogic) paymentNotifyURL(method *payment.Payment) (string, error) {
	return paymentNotifyURL(method.Domain, l.svcCtx.Config.Site.Host, method.Platform, method.Token, report.IsGatewayMode())
}

func (l *PurchaseCheckoutLogic) paymentAmount(method *payment.Payment, current *order.Order, currency string) (int64, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if current.PaymentCurrency != "" {
		if !strings.EqualFold(current.PaymentCurrency, currency) {
			return 0, fmt.Errorf("payment currency differs from recorded checkout")
		}
		return current.PaymentAmount, nil
	}
	amount := current.Amount
	if !strings.EqualFold(currency, l.svcCtx.Config.Currency.Unit) || (method.ExchangeRate > 0 && method.ExchangeRate != 1) {
		converted, err := l.queryPaymentMethodExchangeRate(method, current.Amount, currency)
		if err != nil {
			return 0, err
		}
		if math.IsNaN(converted) || math.IsInf(converted, 0) || converted <= 0 || converted >= float64(math.MaxInt64)/100 {
			return 0, fmt.Errorf("invalid converted payment amount")
		}
		amount = int64(math.Round(converted * 100))
	}
	if strings.EqualFold(method.Platform, "Stripe") {
		amount = stripepayment.RoundHundredths(amount, currency)
	}
	if amount <= 0 {
		return 0, fmt.Errorf("payment amount must be positive")
	}
	err := l.svcCtx.Store.InTx(l.ctx, func(transaction repository.Store) error {
		fresh, err := transaction.Order().FindOneByOrderNoForUpdate(l.ctx, current.OrderNo)
		if err != nil {
			return err
		}
		if fresh.Status != order.StatusPending || fresh.Amount != current.Amount {
			return fmt.Errorf("order changed before checkout")
		}
		if fresh.PaymentCurrency != "" {
			if fresh.PaymentCurrency != currency {
				return fmt.Errorf("payment currency differs from recorded checkout")
			}
			amount = fresh.PaymentAmount
			return nil
		}
		written, err := transaction.Order().SetPaymentExpectation(l.ctx, current.OrderNo, amount, currency)
		if err == nil && !written {
			return fmt.Errorf("payment expectation was not saved")
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	current.PaymentAmount, current.PaymentCurrency = amount, currency
	return amount, nil
}

func (l *PurchaseCheckoutLogic) claimPayment(current *order.Order, tradeNo, session string) error {
	claimed, err := l.svcCtx.Store.Order().ClaimPaymentSession(l.ctx, current.OrderNo, tradeNo, session)
	if err != nil {
		return err
	}
	if !claimed {
		return fmt.Errorf("order no longer accepts this payment session")
	}
	current.TradeNo, current.PaymentSession = tradeNo, session
	return nil
}
