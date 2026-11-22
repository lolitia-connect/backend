package notify

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/payment/cryptomus"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// CryptomusNotifyLogic verifies and settles Cryptomus crypto-payment webhooks.
type CryptomusNotifyLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCryptomusNotifyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CryptomusNotifyLogic {
	return &CryptomusNotifyLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// CryptomusNotify authenticates the webhook payload, re-confirms the payment
// with the gateway and activates the order once the invoice is settled.
func (l *CryptomusNotifyLogic) CryptomusNotify(body []byte) error {
	store := l.svcCtx.Store

	paymentConfig, ok := l.ctx.Value(constant.CtxKeyPayment).(*payment.Payment)
	if !ok || paymentConfig == nil {
		l.Logger.Error("[CryptomusNotify] Payment not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "payment config not found")
	}

	var config payment.CryptomusConfig
	if err := json.Unmarshal([]byte(paymentConfig.Config), &config); err != nil {
		l.Logger.Errorw("[CryptomusNotify] Unmarshal config failed", zap.Any("error", err.Error()))
		return err
	}

	client := cryptomus.NewClient(cryptomus.Config{
		MerchantID: config.MerchantID,
		APIKey:     config.APIKey,
	})

	if !client.VerifyNotificationSign(body) {
		return rejectedNotification{errors.New("verify sign failed")}
	}

	notification, err := cryptomus.ParseNotification(body)
	if err != nil {
		return rejectedNotification{err}
	}
	if notification.OrderNo == "" {
		return rejectedNotification{errors.New("notification has no order number")}
	}

	if !cryptomus.KnownStatus(notification.Status) {
		l.Logger.Warnw("[CryptomusNotify] Unknown payment status", zap.Any("status", notification.Status))
		return nil
	}

	// Wait for a final, paid status; intermediate states must not settle.
	if (!notification.IsFinal || !cryptomus.PaidStatus(notification.Status)) && !cryptomusReviewStatus(notification.Status) {
		l.Logger.Infow("[CryptomusNotify] Payment not settled yet", zap.Any("orderNo", notification.OrderNo), zap.Any("status", notification.Status), zap.Any("isFinal", notification.IsFinal))
		return nil
	}
	orderInfo, err := store.Order().FindOneByOrderNo(l.ctx, notification.OrderNo)
	if err != nil {
		if ent.IsNotFound(err) {
			return rejectedNotification{err}
		}
		return err
	}
	if orderInfo == nil {
		return rejectedNotification{errors.New("order not found")}
	}

	// Re-confirm with the gateway so a spoofed or replayed body cannot settle
	// an order the gateway never marked paid.
	invoice, err := client.GetInvoice(notification.UUID, "")
	if err != nil {
		// An explicit "invoice not found" cannot be retried into existence.
		if cryptomus.IsNotFound(err) {
			l.Logger.Warnw("[CryptomusNotify] Invoice not found at gateway", "orderNo", notification.OrderNo)
			return nil
		}
		l.Logger.Errorw("[CryptomusNotify] Gateway lookup failed", zap.Any("error", err.Error()))
		return err
	}
	if invoice.OrderNo != orderInfo.OrderNo || invoice.UUID != notification.UUID {
		return rejectedNotification{errors.New("invoice does not belong to order")}
	}
	amount, err := cryptomus.ParseMoney(invoice.Amount)
	if err != nil {
		return err
	}
	if !invoice.IsFinal || !invoice.Paid() {
		if cryptomusReviewStatus(invoice.State()) {
			return store.Log().RecordUnmatchedPayment(l.ctx, orderInfo.UserId, log.UnmatchedPaymentFrom(l.ctx, log.UnmatchedPayment{
				OrderNo: orderInfo.OrderNo, TradeNo: invoice.UUID, Platform: paymentConfig.Platform,
				Amount: amount, Currency: invoice.Currency, Reason: "Cryptomus payment requires review: " + invoice.State(),
			}))
		}
		return nil
	}
	activate, err := settlePayment(l.ctx, store, paymentConfig, orderInfo.OrderNo, invoice.UUID, amount, invoice.Currency)
	if err != nil {
		return err
	}

	if activate {
		return enqueueActivation(l.ctx, l.svcCtx, orderInfo.OrderNo)
	}
	return nil
}

func cryptomusReviewStatus(status string) bool {
	switch status {
	case cryptomus.StatusWrongAmount, cryptomus.StatusWrongAmountWaiting, cryptomus.StatusLocked,
		cryptomus.StatusRefundProcess, cryptomus.StatusRefundFail, cryptomus.StatusRefundPaid:
		return true
	default:
		return false
	}
}
