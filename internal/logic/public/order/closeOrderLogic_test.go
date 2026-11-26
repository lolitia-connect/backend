package order

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/internal/model/coupon"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/model/wallet"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/redis/go-redis/v9"
)

// Closing an order restores plan stock. Renewals and traffic resets reference a
// plan but never take stock from it, so they must not be treated as reserving
// ones - doing so inflated plan inventory every time such an order was closed.
func TestReservesPlanInventoryOnlyForNewSubscriptionPurchases(t *testing.T) {
	tests := []struct {
		name    string
		order   *order.Order
		reserve bool
	}{
		{
			name:    "new subscription purchase reserves stock",
			order:   &order.Order{Type: order.TypeSubscribe, SubscribeId: 7},
			reserve: true,
		},
		{
			name:    "renewal does not reserve stock",
			order:   &order.Order{Type: order.TypeRenewal, SubscribeId: 7},
			reserve: false,
		},
		{
			name:    "traffic reset does not reserve stock",
			order:   &order.Order{Type: order.TypeResetTraffic, SubscribeId: 7},
			reserve: false,
		},
		{
			name:    "wallet recharge does not reserve stock",
			order:   &order.Order{Type: order.TypeRecharge, SubscribeId: 7},
			reserve: false,
		},
		{
			name:    "purchase without a plan reserves nothing",
			order:   &order.Order{Type: order.TypeSubscribe},
			reserve: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reservesPlanInventory(tt.order); got != tt.reserve {
				t.Fatalf("reservesPlanInventory(%+v) = %v, want %v", tt.order, got, tt.reserve)
			}
		})
	}
}

type closeOrders struct {
	order.Model
	current order.Order
}

func (model *closeOrders) FindOneByOrderNo(context.Context, string) (*order.Order, error) {
	current := model.current
	return &current, nil
}

func (model *closeOrders) FindOneByOrderNoForUpdate(ctx context.Context, number string) (*order.Order, error) {
	return model.FindOneByOrderNo(ctx, number)
}

func (model *closeOrders) ClosePendingOrder(context.Context, string) (bool, error) {
	if model.current.Status != order.StatusPending {
		return false, nil
	}
	model.current.Status = order.StatusClosed
	return true, nil
}

type closeWallets struct {
	wallet.Model
	current wallet.Wallet
}

func (model *closeWallets) FindOneForUpdate(context.Context, int64) (*wallet.Wallet, error) {
	current := model.current
	return &current, nil
}

func (model *closeWallets) UpdateBalanceFields(_ context.Context, current *wallet.Wallet) error {
	model.current = *current
	return nil
}

type closePlans struct {
	subscribe.Model
	restored int
}

type closeCoupons struct {
	coupon.Model
	released int
}

func (model *closeCoupons) ReleaseUsage(context.Context, string) error {
	model.released++
	return nil
}

func (model *closePlans) RestoreInventory(context.Context, int64) error {
	model.restored++
	return nil
}

type closePayments struct {
	payment.Model
	method   payment.Payment
	findErr  error
	nilMatch bool
}

func (model *closePayments) FindOne(context.Context, int64) (*payment.Payment, error) {
	if model.findErr != nil {
		return nil, model.findErr
	}
	if model.nilMatch {
		return nil, nil
	}
	return &model.method, nil
}

type closeLogs struct {
	log.Model
	writes    int
	insertErr error
}

func (model *closeLogs) Insert(context.Context, *log.SystemLog) error {
	if model.insertErr != nil {
		return model.insertErr
	}
	model.writes++
	return nil
}

type closeStore struct {
	repository.Store
	orders   closeOrders
	wallets  closeWallets
	plans    closePlans
	payments closePayments
	logs     closeLogs
	coupons  closeCoupons
}

func (store *closeStore) Order() order.Model         { return &store.orders }
func (store *closeStore) Wallet() wallet.Model       { return &store.wallets }
func (store *closeStore) Subscribe() subscribe.Model { return &store.plans }
func (store *closeStore) Payment() payment.Model     { return &store.payments }
func (store *closeStore) Log() log.Model             { return &store.logs }
func (store *closeStore) Coupon() coupon.Model       { return &store.coupons }

func (store *closeStore) InTx(_ context.Context, action func(repository.Store) error) error {
	current, funds, restored, logs := store.orders.current, store.wallets.current, store.plans.restored, store.logs.writes
	released := store.coupons.released
	if err := action(store); err != nil {
		store.orders.current, store.wallets.current, store.plans.restored, store.logs.writes = current, funds, restored, logs
		store.coupons.released = released
		return err
	}
	return nil
}

func TestCloseReturnsGiftAndInventoryOnceAfterRetry(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := &closeStore{
		orders:  closeOrders{current: order.Order{OrderNo: "cancel", Status: order.StatusPending, Type: order.TypeSubscribe, Method: "balance", UserId: 7, SubscribeId: 9, GiftAmount: 50, Coupon: "SAVE", CouponReserved: true}},
		wallets: closeWallets{current: wallet.Wallet{UserId: 7, GiftAmount: 10, Balance: 20}},
	}
	service := &svc.ServiceContext{Store: store, Redis: client}
	store.logs.insertErr = errors.New("audit unavailable")
	if err := NewCloseOrderLogic(context.Background(), service).CloseOrder(&types.CloseOrderRequest{OrderNo: "cancel"}); err == nil {
		t.Fatal("failed refund audit was acknowledged")
	}
	if store.orders.current.Status != order.StatusPending || store.wallets.current.GiftAmount != 10 || store.plans.restored != 0 || store.coupons.released != 0 {
		t.Fatal("failed close partially released funds, coupon or inventory")
	}
	store.logs.insertErr = nil
	for attempt := 0; attempt < 2; attempt++ {
		if err := NewCloseOrderLogic(context.Background(), service).CloseOrder(&types.CloseOrderRequest{OrderNo: "cancel"}); err != nil {
			t.Fatal(err)
		}
	}
	if store.orders.current.Status != order.StatusClosed || store.wallets.current.GiftAmount != 60 || store.wallets.current.Balance != 20 || store.plans.restored != 1 || store.logs.writes != 1 || store.coupons.released != 1 {
		t.Fatal("repeated close released funds, coupon or inventory more than once")
	}
}

func TestEPayCloseRequiresConfirmedUnpaidStateForExpiry(t *testing.T) {
	for _, test := range []struct {
		name, response string
		expiry         bool
		age            time.Duration
		wantErr        bool
	}{
		{"unavailable expiry", `{"code":-1}`, true, time.Hour, true},
		{"unavailable explicit", `{"code":-1}`, false, time.Hour, false},
		{"unpaid too young", `{"code":1,"pid":"1001","out_trade_no":"cancel","status":0}`, true, 20 * time.Minute, true},
		{"unpaid expired", `{"code":1,"pid":"1001","out_trade_no":"cancel","status":0}`, true, time.Hour, false},
		{"unknown status", `{"code":1,"pid":"1001","out_trade_no":"cancel","status":2}`, true, time.Hour, true},
		{"wrong order", `{"code":1,"pid":"1001","out_trade_no":"other","status":0}`, true, time.Hour, true},
		{"wrong merchant", `{"code":1,"pid":"other","out_trade_no":"cancel","status":0}`, true, time.Hour, true},
		{"paid without type", `{"code":1,"pid":"1001","out_trade_no":"cancel","status":1}`, false, time.Hour, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			gateway := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte(test.response)) }))
			defer gateway.Close()
			config, err := json.Marshal(payment.EPayConfig{Pid: "1001", Key: "secret", Url: gateway.URL, Type: "alipay"})
			if err != nil {
				t.Fatal(err)
			}
			store := &closeStore{payments: closePayments{method: payment.Payment{Config: string(config)}}}
			logic := NewCloseOrderLogic(context.Background(), &svc.ServiceContext{Store: store})
			logic.expiry = test.expiry
			err = logic.closeGateway(&order.Order{OrderNo: "cancel", Method: "EPay", CreatedAt: time.Now().Add(-test.age)})
			if (err != nil) != test.wantErr {
				t.Fatalf("close error = %v; want error %t", err, test.wantErr)
			}
		})
	}
}

func TestCloseGatewayRejectsUnverifiablePaymentConfiguration(t *testing.T) {
	lookupErr := errors.New("payment lookup unavailable")
	for _, tc := range []struct {
		name    string
		method  string
		config  string
		trade   string
		findErr error
		wantErr bool
	}{
		{name: "balance is already settled", method: "balance", wantErr: false},
		{name: "unstarted Stripe checkout", method: "Stripe", wantErr: false},
		{name: "payment lookup failure", method: "Stripe", trade: "cs_fixture", findErr: lookupErr, wantErr: true},
		{name: "missing payment method", method: "Stripe", trade: "cs_fixture", wantErr: true},
		{name: "invalid Stripe configuration", method: "Stripe", config: "{", trade: "cs_fixture", wantErr: true},
		{name: "invalid Alipay configuration", method: "AlipayF2F", config: "{", wantErr: true},
		{name: "invalid Alipay Plus configuration", method: "AlipayPlus", config: "{", wantErr: true},
		{name: "invalid Cryptomus configuration", method: "Cryptomus", config: "{", trade: "invoice", wantErr: true},
		{name: "invalid EPay configuration", method: "EPay", config: "{", wantErr: true},
		{name: "invalid CryptoSaaS configuration", method: "CryptoSaaS", config: "{", wantErr: true},
		{name: "unsupported platform", method: "unknown", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &closeStore{payments: closePayments{method: payment.Payment{Config: tc.config}, findErr: tc.findErr, nilMatch: tc.name == "missing payment method"}}
			logic := NewCloseOrderLogic(context.Background(), &svc.ServiceContext{Store: store})
			err := logic.closeGateway(&order.Order{OrderNo: "close-fixture", Method: tc.method, TradeNo: tc.trade})
			if (err != nil) != tc.wantErr {
				t.Fatalf("closeGateway error = %v, want error %t", err, tc.wantErr)
			}
			if tc.findErr != nil && !errors.Is(err, tc.findErr) {
				t.Fatalf("closeGateway error = %v, want %v", err, tc.findErr)
			}
		})
	}
}
