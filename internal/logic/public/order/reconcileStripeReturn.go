package order

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/logic/notify"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/types"
	platform "github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/payment/stripe"
	"github.com/perfect-panel/server/pkg/xerr"
)

// Reconcile is the fast path for a returning Checkout customer. Only the Session
// already bound to the authorized order can confirm payment.
func (l *V2OrderLogic) Reconcile(orderNo, checkoutToken string) (*types.V2OrderResponse, error) {
	current, err := l.svcCtx.Store.Order().FindOneByOrderNo(l.ctx, orderNo)
	if ent.IsNotFound(err) || (err == nil && current == nil) {
		return nil, xerr.NewErrCode(xerr.OrderNotExist)
	}
	if err != nil {
		return nil, err
	}
	if err := l.authorizeOrder(current, checkoutToken); err != nil {
		return nil, err
	}
	if current.Status != order.StatusPending || platform.ParsePlatform(current.Method) != platform.Stripe || !strings.HasPrefix(current.TradeNo, "cs_") {
		return l.snapshotResponse(current, checkoutToken)
	}

	// Simultaneous tabs share one lookup. A failed lookup leaves the webhook and
	// scheduled reconciliation available; a refresh after the cooldown can retry.
	ctx, cancel := context.WithTimeout(l.ctx, 4*time.Second)
	defer cancel()
	if l.svcCtx.Redis != nil {
		cooldown, done := context.WithTimeout(ctx, 250*time.Millisecond)
		acquired, cacheErr := l.svcCtx.Redis.SetNX(cooldown, "order:stripe-return:"+current.OrderNo, "1", 5*time.Second).Result()
		done()
		if cacheErr == nil && !acquired {
			return l.GetOrder(orderNo, checkoutToken)
		}
	}
	if err := l.confirmStripeSession(ctx, current); err != nil {
		l.Logger.Warnw("Stripe return could not confirm order", "order_no", orderNo, "error", err)
	}
	// The webhook or the activation worker may have committed while Stripe replied.
	return l.GetOrder(orderNo, checkoutToken)
}

func (l *V2OrderLogic) confirmStripeSession(ctx context.Context, current *order.Order) error {
	method, err := l.svcCtx.Store.Payment().FindOne(ctx, current.PaymentId)
	if err != nil {
		return err
	}
	var config payment.StripeConfig
	if method == nil || platform.ParsePlatform(method.Platform) != platform.Stripe || config.Unmarshal([]byte(method.Config)) != nil || config.SecretKey == "" {
		return errors.New("invalid Stripe payment method")
	}
	receipt, err := stripe.NewClient(stripe.Config{SecretKey: config.SecretKey}).QueryPayment(ctx, current.TradeNo)
	if err != nil {
		return errors.New("Stripe Session lookup failed")
	}
	if receipt.OrderNo != current.OrderNo || receipt.TradeNo != current.TradeNo {
		return errors.New("Stripe Session does not match the order")
	}
	if !receipt.Paid {
		return nil
	}
	return notify.SettleConfirmedPayment(l.ctx, l.svcCtx, method, current.OrderNo, receipt.TradeNo, receipt.Amount, receipt.Currency)
}
