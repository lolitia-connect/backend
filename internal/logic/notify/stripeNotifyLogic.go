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

	// Skip events that don't need processing (e.g. payment_failed, unhandled types)
	if notify.EventType != "payment_intent.succeeded" && notify.EventType != "checkout.session.completed" && notify.EventType != "checkout.session.async_payment_succeeded" {
		return nil
	}
	if !notify.Paid {
		return nil
	}

	// Validate order number
	if notify.OrderNo == "" {
		return rejectedNotification{errors.New("order_no is empty in webhook")}
	}

	orderInfo, err := store.Order().FindOneByOrderNo(l.ctx, notify.OrderNo)
	if ent.IsNotFound(err) || (err == nil && orderInfo == nil) {
		return rejectedNotification{xerr.NewErrCode(xerr.OrderNotExist)}
	}
	if err != nil {
		l.Logger.Errorw("[StripeNotify] Find order failed", "error", err)
		return err
	}
	if strings.HasPrefix(orderInfo.TradeNo, "cs_") && notify.EventType == "payment_intent.succeeded" {
		return nil
	}
	activate, err := settlePayment(l.ctx, store, stripeConfig, notify.OrderNo, notify.TradeNo, notify.Amount, notify.Currency)
	if err != nil {
		return err
	}
	if activate {
		return enqueueActivation(l.ctx, l.svcCtx, notify.OrderNo)
	}
	return nil
}
