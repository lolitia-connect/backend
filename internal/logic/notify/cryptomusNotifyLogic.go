package notify

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/payment/cryptomus"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	queueType "github.com/perfect-panel/server/queue/types"
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
	if !ok {
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

	if !client.VerifyNotificationSign(body) && !l.svcCtx.Config.Debug {
		l.Logger.Error("[CryptomusNotify] Verify sign failed")
		return errors.New("verify sign failed")
	}

	notification, err := cryptomus.ParseNotification(body)
	if err != nil {
		l.Logger.Errorw("[CryptomusNotify] Parse notification failed", zap.Any("error", err.Error()))
		return err
	}
	if notification.OrderNo == "" {
		l.Logger.Error("[CryptomusNotify] Notification has no order number")
		return errors.New("notification has no order number")
	}

	orderInfo, err := store.Order().FindOneByOrderNo(l.ctx, notification.OrderNo)
	if err != nil {
		l.Logger.Error("[CryptomusNotify] Find order failed", zap.Any("error", err.Error()), zap.Any("orderNo", notification.OrderNo))
		return errors.Wrapf(xerr.NewErrCode(xerr.OrderNotExist), "order not exist: %v", notification.OrderNo)
	}

	// Only pending orders (status 1) may be settled; anything else is a
	// duplicate or out-of-order notification.
	if orderInfo.Status != 1 {
		l.Logger.Infow("[CryptomusNotify] Order is not pending, skipping", zap.Any("orderNo", notification.OrderNo), zap.Any("status", orderInfo.Status))
		return nil
	}

	if !cryptomus.KnownStatus(notification.Status) {
		l.Logger.Warnw("[CryptomusNotify] Unknown payment status", zap.Any("status", notification.Status))
		return nil
	}

	// Wait for a final, paid status; intermediate states must not settle.
	if !notification.IsFinal || !cryptomus.PaidStatus(notification.Status) {
		l.Logger.Infow("[CryptomusNotify] Payment not settled yet", zap.Any("orderNo", notification.OrderNo), zap.Any("status", notification.Status), zap.Any("isFinal", notification.IsFinal))
		return nil
	}

	// Re-confirm with the gateway so a spoofed or replayed body cannot settle
	// an order the gateway never marked paid.
	invoice, err := client.GetInvoice(notification.UUID, "")
	if err != nil {
		// An explicit "invoice not found" cannot be retried into existence.
		if cryptomus.IsNotFound(err) {
			l.Logger.Errorw("[CryptomusNotify] Invoice not found at gateway", zap.Any("orderNo", notification.OrderNo), zap.Any("uuid", notification.UUID))
			return nil
		}
		l.Logger.Errorw("[CryptomusNotify] Gateway lookup failed", zap.Any("error", err.Error()))
		return err
	}
	if invoice.OrderNo != orderInfo.OrderNo {
		l.Logger.Errorw("[CryptomusNotify] Invoice order mismatch", zap.Any("orderNo", orderInfo.OrderNo), zap.Any("invoiceOrder", invoice.OrderNo))
		return errors.New("invoice does not belong to order")
	}
	if !invoice.Paid() {
		l.Logger.Infow("[CryptomusNotify] Gateway reports unsettled invoice", zap.Any("orderNo", orderInfo.OrderNo), zap.Any("state", invoice.State()))
		return nil
	}

	// Verify the invoice charges the amount and currency this payment method
	// is configured for before settling.
	expectedCurrency := strings.ToUpper(strings.TrimSpace(paymentConfig.CurrencyUnit))
	if expectedCurrency == "" {
		expectedCurrency = strings.ToUpper(l.svcCtx.Config.Currency.Unit)
	}
	if !strings.EqualFold(invoice.Currency, expectedCurrency) {
		l.Logger.Errorw("[CryptomusNotify] Invoice currency mismatch", zap.Any("want", expectedCurrency), zap.Any("got", invoice.Currency))
		return errors.New("invoice currency mismatch")
	}
	if strings.EqualFold(expectedCurrency, l.svcCtx.Config.Currency.Unit) {
		amount, amountErr := cryptomus.ParseMoney(invoice.Amount)
		if amountErr != nil {
			l.Logger.Errorw("[CryptomusNotify] Parse invoice amount failed", zap.Any("error", amountErr.Error()))
			return errors.New("invalid invoice amount")
		}
		if amount != orderInfo.Amount {
			l.Logger.Errorw("[CryptomusNotify] Invoice amount mismatch", zap.Any("want", orderInfo.Amount), zap.Any("got", amount))
			return errors.New("invoice amount mismatch")
		}
	}

	// Update order status and hand off to the activation worker.
	if err = store.Order().UpdateOrderStatus(l.ctx, orderInfo.OrderNo, 2); err != nil {
		l.Logger.Error("[CryptomusNotify] Update order status failed", zap.Any("error", err.Error()), zap.Any("orderNo", orderInfo.OrderNo))
		return err
	}

	payload := queueType.ForthwithActivateOrderPayload{OrderNo: orderInfo.OrderNo}
	bytes, err := json.Marshal(&payload)
	if err != nil {
		l.Logger.Error("[CryptomusNotify] Marshal payload failed", zap.Any("error", err.Error()))
		return err
	}
	task := asynq.NewTask(queueType.ForthwithActivateOrder, bytes, asynq.MaxRetry(5))
	taskInfo, err := l.svcCtx.Queue.EnqueueContext(l.ctx, task)
	if err != nil {
		l.Logger.Error("[CryptomusNotify] Enqueue task failed", zap.Any("error", err.Error()))
		return err
	}
	l.Logger.Info("[CryptomusNotify] Enqueue task success", zap.Any("taskInfo", taskInfo))
	return nil
}
