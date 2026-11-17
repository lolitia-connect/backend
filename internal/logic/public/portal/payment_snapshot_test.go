package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/requestmeta"
)

func TestPaymentNotifyURLUsesConfiguredOrigin(t *testing.T) {
	for _, test := range []struct {
		domain  string
		host    string
		gateway bool
		want    string
	}{
		{"", "panel.example\nbackup.example", false, "https://panel.example/v1/notify/epay/token"},
		{"https://pay.example/base/", "panel.example", true, "https://pay.example/base/api/v1/notify/epay/token"},
		{"", "", false, ""},
		{"javascript:invalid", "", false, ""},
		{"https://user:password@pay.example", "", false, ""},
		{"https://pay.example?redirect=other", "", false, ""},
	} {
		got, err := paymentNotifyURL(test.domain, test.host, "epay", "token", test.gateway)
		if test.want == "" {
			if err == nil {
				t.Fatalf("accepted callback origin %q", test.domain)
			}
		} else if err != nil || got != test.want {
			t.Fatalf("callback URL = %q, %v; want %q", got, err, test.want)
		}
	}
}

type checkoutOrders struct {
	order.Model
	current     order.Order
	begins      int
	completions int
}

func (model *checkoutOrders) BeginPaymentSession(_ context.Context, orderNo, session string) (bool, error) {
	model.begins++
	if model.current.OrderNo != orderNo || model.current.Status != order.StatusPending || model.current.PaymentSession != "" || model.current.TradeNo != "" {
		return false, nil
	}
	model.current.PaymentSession = session
	return true, nil
}

func (model *checkoutOrders) CompletePaymentSession(_ context.Context, orderNo, expectedSession, tradeNo, session string) (bool, error) {
	model.completions++
	if model.current.OrderNo != orderNo || model.current.Status != order.StatusPending || model.current.PaymentSession != expectedSession {
		return false, nil
	}
	model.current.TradeNo, model.current.PaymentSession = tradeNo, session
	return true, nil
}

type checkoutStore struct {
	repository.Store
	orders *checkoutOrders
}

func (store *checkoutStore) Order() order.Model { return store.orders }

func TestEPayCheckoutPersistsOneAttemptAcrossRequests(t *testing.T) {
	for _, test := range []struct {
		name, response string
		ready          bool
	}{
		{"ready", `{"code":1,"trade_no":"trade","qrcode":"https://cashier.example/qr"}`, true},
		{"lost response", `{"code":1`, false},
		{"ambiguous response", `{"code":1,"trade_no":"trade","payurl":"https://pay.example","qrcode":"qr"}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			orders := &checkoutOrders{current: order.Order{OrderNo: "order", Status: order.StatusPending, PaymentAmount: 1029, PaymentCurrency: "CNY"}}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests++
				if orders.current.PaymentSession == "" {
					t.Error("sent create before saving the attempt")
				}
				if request.URL.Path != "/shop/mapi.php" {
					t.Errorf("unexpected create path %s", request.URL.Path)
				}
				writer.Write([]byte(test.response))
			}))
			defer server.Close()
			service := &svc.ServiceContext{Store: &checkoutStore{orders: orders}}
			service.Config.Site.SiteName, service.Config.Site.Host = "Plan", "panel.example"
			method := &payment.Payment{Platform: "EPay", Domain: server.URL, Config: `{"pid":"1001","url":"/shop","key":"key","type":"alipay","checkout_mode":"api"}`}
			method.Domain = "https://panel.example"
			method.Config = `{"pid":"1001","url":"` + server.URL + `/shop","key":"key","type":"alipay","checkout_mode":"api"}`
			ctx := requestmeta.With(context.Background(), requestmeta.New("203.0.113.9", "test"))
			for attempt := 0; attempt < 2; attempt++ {
				current := orders.current
				response, err := NewPurchaseCheckoutLogic(ctx, service).epayPayment(method, &current, "")
				if test.ready {
					if err != nil || response == nil || response.Type != "qr" || response.CheckoutUrl != "https://cashier.example/qr" {
						t.Fatalf("attempt %d: %#v, %v", attempt, response, err)
					}
				} else if err == nil || response != nil {
					t.Fatalf("ambiguous attempt %d returned a payment entry", attempt)
				}
			}
			if requests != 1 || orders.begins != 1 {
				t.Fatalf("replayed create: requests=%d, begins=%d", requests, orders.begins)
			}
			if test.ready && (orders.completions != 1 || orders.current.TradeNo != "trade") {
				t.Fatal("ready session was not persisted")
			}
			if !test.ready && (orders.completions != 0 || orders.current.PaymentSession != `{"state":"creating"}`) {
				t.Fatal("ambiguous create lost its durable attempt")
			}
		})
	}
}
