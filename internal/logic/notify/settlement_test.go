package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/payment/cryptomus"
	stripeapi "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/webhook"
)

type settlementOrders struct {
	order.Model
	current *order.Order
	writes  int
	reads   int
}

func (model *settlementOrders) FindOneByOrderNoForUpdate(context.Context, string) (*order.Order, error) {
	model.reads++
	return model.current, nil
}

func (model *settlementOrders) FindOneByOrderNo(context.Context, string) (*order.Order, error) {
	model.reads++
	return model.current, nil
}

func (model *settlementOrders) MarkOrderPaid(_ context.Context, _ string, tradeNo string) (bool, error) {
	model.writes++
	model.current.Status = order.StatusPaid
	model.current.TradeNo = tradeNo
	return true, nil
}

type settlementStore struct {
	repository.Store
	orders    *settlementOrders
	unmatched []log.UnmatchedPayment
}

type settlementLogs struct {
	log.Model
	store *settlementStore
}

func (model *settlementLogs) RecordUnmatchedPayment(_ context.Context, _ int64, receipt log.UnmatchedPayment) error {
	model.store.unmatched = append(model.store.unmatched, receipt)
	return nil
}

func (store *settlementStore) Log() log.Model { return &settlementLogs{store: store} }

func (store *settlementStore) Order() order.Model { return store.orders }

func (store *settlementStore) InTx(_ context.Context, apply func(repository.Store) error) error {
	return apply(store)
}

func TestSettlementRejectsUnrelatedAndClosedPayments(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      uint8
		paymentID   int64
		storedTrade string
	}{
		{"closed", order.StatusClosed, 7, "trade"},
		{"wrong payment method", order.StatusPending, 8, "trade"},
		{"claimed by another trade", order.StatusPending, 7, "other"},
		{"finished by another trade", order.StatusFinished, 7, "other"},
	} {
		t.Run(test.name, func(t *testing.T) {
			orders := &settlementOrders{current: &order.Order{Status: test.status, PaymentId: test.paymentID, TradeNo: test.storedTrade, Amount: 100}}
			store := &settlementStore{orders: orders}
			activate, err := settlePayment(context.Background(), store, &payment.Payment{Id: 7}, "order", "trade", 100, "USD")
			if !IsRejectedNotification(err) || activate || orders.writes != 0 {
				t.Fatalf("unmatched payment: activate=%t, err=%v, writes=%d", activate, err, orders.writes)
			}
			if len(store.unmatched) != 1 {
				t.Fatal("unmatched payment was not audited")
			}
		})
	}
}

func TestSettlementBindsTradeAndRetriesActivationWithoutAnotherPayment(t *testing.T) {
	orders := &settlementOrders{current: &order.Order{Status: order.StatusPending, PaymentId: 7, Amount: 100}}
	store := &settlementStore{orders: orders}
	for attempt := 0; attempt < 2; attempt++ {
		activate, err := settlePayment(context.Background(), store, &payment.Payment{Id: 7}, "order", "trade", 100, "USD")
		if err != nil || !activate || orders.writes != 1 || orders.current.TradeNo != "trade" {
			t.Fatalf("attempt %d: activate %t, error %v, writes %d", attempt, activate, err, orders.writes)
		}
	}
	orders.current.Status = order.StatusFinished
	activate, err := settlePayment(context.Background(), store, &payment.Payment{Id: 7}, "order", "trade", 100, "USD")
	if err != nil || activate || orders.writes != 1 {
		t.Fatalf("finished payment retried: %t, %v", activate, err)
	}
}

func TestSettlementUsesRecordedAmountInsteadOfCurrentMethodCurrency(t *testing.T) {
	for _, receipt := range []struct {
		amount   int64
		currency string
	}{{99, "USD"}, {100, "EUR"}} {
		orders := &settlementOrders{current: &order.Order{Status: order.StatusPending, PaymentId: 7, PaymentAmount: 100, PaymentCurrency: "USD"}}
		store := &settlementStore{orders: orders}
		activate, err := settlePayment(context.Background(), store, &payment.Payment{Id: 7, CurrencyUnit: "EUR"}, "order", "trade", receipt.amount, receipt.currency)
		if err == nil || activate || orders.writes != 0 || len(store.unmatched) != 1 {
			t.Fatal("mismatched payment accepted or not audited")
		}
	}
}

func TestDebugDoesNotBypassGatewaySignatures(t *testing.T) {
	service := &svc.ServiceContext{}
	service.Config.Debug = true
	ctx := context.WithValue(context.Background(), constant.CtxKeyPayment, &payment.Payment{Platform: "EPay", Config: `{"pid":"merchant","key":"secret","url":"https://pay.example"}`})
	if err := NewEPayNotifyLogic(ctx, service, EPayNotifyMeta{Params: map[string]string{}}).EPayNotify(&types.EPayNotifyRequest{}); !IsRejectedNotification(err) {
		t.Fatal("debug mode bypassed EPay signature verification")
	}
	ctx = context.WithValue(context.Background(), constant.CtxKeyPayment, &payment.Payment{Platform: "Cryptomus", Config: `{"merchant_id":"merchant","api_key":"secret"}`})
	if err := NewCryptomusNotifyLogic(ctx, service).CryptomusNotify([]byte(`{"order_id":"order","sign":"invalid"}`)); !IsRejectedNotification(err) {
		t.Fatal("debug mode bypassed Cryptomus signature verification")
	}
}

func TestCryptomusReviewStatuses(t *testing.T) {
	for _, status := range []string{cryptomus.StatusWrongAmount, cryptomus.StatusWrongAmountWaiting, cryptomus.StatusLocked, cryptomus.StatusRefundProcess, cryptomus.StatusRefundFail, cryptomus.StatusRefundPaid} {
		if !cryptomusReviewStatus(status) {
			t.Fatalf("received-fund state %s was ignored", status)
		}
	}
	for _, status := range []string{cryptomus.StatusProcess, cryptomus.StatusCancel, cryptomus.StatusPaid, "unknown"} {
		if cryptomusReviewStatus(status) {
			t.Fatalf("state %s incorrectly marked for review", status)
		}
	}
}

func TestStripeAsyncSuccessReachesOrderSettlement(t *testing.T) {
	for _, amount := range []int64{100, 99} {
		orders := &settlementOrders{current: &order.Order{OrderNo: "order", Status: order.StatusFinished, PaymentId: 7, TradeNo: "cs_test", PaymentAmount: 100, PaymentCurrency: "USD"}}
		store := &settlementStore{orders: orders}
		method := &payment.Payment{Id: 7, Platform: "Stripe", Config: `{"webhook_secret":"signing-secret"}`}
		ctx := context.WithValue(context.Background(), constant.CtxKeyPayment, method)
		payload, err := json.Marshal(map[string]any{
			"id": "evt_async", "object": "event", "type": "checkout.session.async_payment_succeeded", "api_version": stripeapi.APIVersion,
			"data": map[string]any{"object": map[string]any{"id": "cs_test", "client_reference_id": "order", "payment_status": "paid", "amount_total": amount, "currency": "usd"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("POST", "/notify", strings.NewReader(string(payload))).WithContext(ctx)
		now := time.Now()
		request.Header.Set("Stripe-Signature", fmt.Sprintf("t=%d,v1=%x", now.Unix(), webhook.ComputeSignature(now, payload, "signing-secret")))
		err = NewStripeNotifyLogic(ctx, &svc.ServiceContext{Store: store}).StripeNotify(request, httptest.NewRecorder())
		if orders.reads != 2 {
			t.Fatal("async success was filtered before order settlement")
		}
		if (err != nil) != (amount != 100) {
			t.Fatalf("amount %d: %v", amount, err)
		}
		if amount != 100 && len(store.unmatched) != 1 {
			t.Fatal("async payment mismatch was not audited")
		}
	}
}
