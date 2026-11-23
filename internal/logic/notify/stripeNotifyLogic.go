package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/payment/stripe"
	"go.uber.org/zap"
)

type StripeNotifyLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewStripeNotifyLogic Stripe notify
func NewStripeNotifyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StripeNotifyLogic {
	return &StripeNotifyLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *StripeNotifyLogic) StripeNotify(r *http.Request, w http.ResponseWriter) error {
	store := l.svcCtx.Store
	const MaxBodyBytes = int64(65536)
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	webhookPayload, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return rejectedNotification{err}
		}
		return err
	}
	signature := r.Header.Get("Stripe-Signature")
	stripeConfig, ok := l.ctx.Value(constant.CtxKeyPayment).(*payment.Payment)
	if !ok || stripeConfig == nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "payment config not found")
	}
	config := payment.StripeConfig{}
	if err := json.Unmarshal([]byte(stripeConfig.Config), &config); err != nil {
		return err
	}
	client := stripe.NewClient(stripe.Config{
		PublicKey:     config.PublicKey,
		SecretKey:     config.SecretKey,
		WebhookSecret: config.WebhookSecret,
	})

	notify, err := client.ParseNotify(webhookPayload, signature)
	if err != nil {
		return rejectedNotification{err}
	}

	asyncFailed := notify.EventType == "checkout.session.async_payment_failed"
	if !asyncFailed && !notify.Paid {
		return nil
	}

	// Validate order number
	if notify.OrderNo == "" {
		return rejectedNotification{errors.New("order_no is empty in webhook")}
	}

	if asyncFailed || notify.EventType == "payment_intent.succeeded" {
		orderInfo, err := store.Order().FindOneByOrderNo(l.ctx, notify.OrderNo)
		if ent.IsNotFound(err) || (err == nil && orderInfo == nil) {
			return rejectedNotification{xerr.NewErrCode(xerr.OrderNotExist)}
		}
		if err != nil {
			return err
		}
		if asyncFailed {
			if orderInfo.Status != order.StatusPending || orderInfo.PaymentId != stripeConfig.Id || orderInfo.TradeNo != notify.TradeNo || !strings.HasPrefix(notify.TradeNo, "cs_") {
				return nil
			}
			return EnqueueStripeCloseCheck(l.ctx, l.svcCtx, orderInfo.OrderNo)
		}
		// Only legacy orders bound to an intent can settle from pi_ events. New orders bind to a Session.
		if !strings.HasPrefix(orderInfo.TradeNo, "pi_") {
			return nil
		}
	}
	return SettleConfirmedPayment(l.ctx, l.svcCtx, stripeConfig, notify.OrderNo, notify.TradeNo, notify.Amount, notify.Currency)
}
