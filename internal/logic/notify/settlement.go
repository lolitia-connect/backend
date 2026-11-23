package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/orderstream"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/queue/types"
)

type rejectedNotification struct{ error }

func (r rejectedNotification) Unwrap() error { return r.error }

func IsRejectedNotification(err error) bool {
	var rejection rejectedNotification
	return errors.As(err, &rejection)
}

func settlePayment(ctx context.Context, store repository.Store, method *payment.Payment, orderNo, tradeNo string, amount int64, currency string) (bool, error) {
	activate := false
	var rejection error
	err := store.InTx(ctx, func(transaction repository.Store) error {
		current, err := transaction.Order().FindOneByOrderNoForUpdate(ctx, orderNo)
		if err != nil {
			if ent.IsNotFound(err) {
				return rejectedNotification{err}
			}
			return err
		}
		if current == nil || method == nil || tradeNo == "" {
			return rejectedNotification{errors.New("payment does not belong to order")}
		}
		reject := func(reason string) error {
			rejection = rejectedNotification{errors.New(reason)}
			return transaction.Log().RecordUnmatchedPayment(ctx, current.UserId, log.UnmatchedPaymentFrom(ctx, log.UnmatchedPayment{
				OrderNo: orderNo, TradeNo: tradeNo, Platform: method.Platform, Amount: amount,
				Currency: strings.ToUpper(currency), Reason: reason,
			}))
		}
		if current.PaymentId != method.Id {
			return reject("payment method does not match order")
		}
		expectedAmount, expectedCurrency := current.PaymentAmount, current.PaymentCurrency
		if expectedCurrency == "" {
			expectedAmount, expectedCurrency = current.Amount, method.CurrencyUnit
		}
		if amount <= 0 || amount != expectedAmount || (expectedCurrency != "" && !strings.EqualFold(currency, expectedCurrency)) {
			return reject("payment amount or currency does not match order")
		}
		if current.TradeNo != "" && current.TradeNo != tradeNo {
			return reject("payment trade number does not match order")
		}
		switch current.Status {
		case order.StatusFinished:
			return nil
		case order.StatusPaid:
			activate = true
			return nil
		case order.StatusPending:
			activate, err = transaction.Order().MarkOrderPaid(ctx, orderNo, tradeNo)
			if err == nil && !activate {
				return fmt.Errorf("order changed during payment settlement")
			}
			return err
		default:
			return reject(fmt.Sprintf("order cannot accept payment in state %d", current.Status))
		}
	})
	if err == nil {
		err = rejection
	}
	return activate, err
}

func enqueueActivation(ctx context.Context, service *svc.ServiceContext, orderNo string) error {
	payload, err := json.Marshal(types.ForthwithActivateOrderPayload{OrderNo: orderNo})
	if err != nil {
		return err
	}
	enqueueCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_, err = service.Queue.EnqueueContext(enqueueCtx, asynq.NewTask(
		types.ForthwithActivateOrder,
		payload,
		asynq.MaxRetry(5),
		asynq.Unique(5*time.Minute),
	))
	if errors.Is(err, asynq.ErrDuplicateTask) {
		err = nil
	}
	if err != nil {
		return err
	}
	if service.Redis != nil {
		orderstream.Wake(ctx, service.Redis, orderNo)
	}
	return nil
}

func SettleConfirmedPayment(ctx context.Context, service *svc.ServiceContext, method *payment.Payment, orderNo, tradeNo string, amount int64, currency string) error {
	activate, err := settlePayment(ctx, service.Store, method, orderNo, tradeNo, amount, currency)
	if err != nil || !activate {
		return err
	}
	return enqueueActivation(ctx, service, orderNo)
}

func EnqueuePaidOrder(ctx context.Context, service *svc.ServiceContext, orderNo string) error {
	return enqueueActivation(ctx, service, orderNo)
}

func EnqueueStripeCloseCheck(ctx context.Context, service *svc.ServiceContext, orderNo string) error {
	payload, err := json.Marshal(types.DeferCloseOrderPayload{OrderNo: orderNo})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_, err = service.Queue.EnqueueContext(ctx, asynq.NewTask(types.DeferCloseOrder, payload), asynq.MaxRetry(0), asynq.Unique(5*time.Minute))
	if errors.Is(err, asynq.ErrDuplicateTask) {
		return nil
	}
	return err
}

func configCurrency(configured, fallback string) string {
	if strings.TrimSpace(configured) == "" {
		return strings.ToUpper(fallback)
	}
	return strings.ToUpper(configured)
}
