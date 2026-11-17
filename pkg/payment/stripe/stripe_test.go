package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/webhook"
)

func TestStripeAlipay(t *testing.T) {
	t.Skipf("Skip TestStripeAlipay test")
	client := NewClient(Config{
		WebhookSecret: "",
	})
	order := Order{
		OrderNo:   "JS20210719123456789",
		Subscribe: "测试",
		Amount:    100,
		Currency:  string(stripe.CurrencyGBP),
		Payment:   "alipay",
	}
	user := User{
		UserId: 1,
		Email:  "tension@ppanel.dev",
	}
	result, err := client.CreateCheckoutSession(&order, &user, "https://example.com/success", "https://example.com/cancel")
	if err != nil {
		t.Error(err.Error())
	}
	t.Logf("CheckoutURL: %s\n", result.CheckoutURL)
}

func TestStripeWechat(t *testing.T) {
	t.Skipf("Skip TestStripeWechat test")
	client := NewClient(Config{
		SecretKey:     "SecretKey",
		PublicKey:     "PublicKey",
		WebhookSecret: "",
	})
	order := Order{
		OrderNo:   "JS20210719123456789",
		Subscribe: "测试",
		Amount:    100,
		Currency:  string(stripe.CurrencyGBP),
		Payment:   "wechat_pay",
	}
	user := User{
		UserId: 1,
		Email:  "tension@ppanel.dev",
	}
	result, err := client.CreateCheckoutSession(&order, &user, "https://example.com/success", "https://example.com/cancel")
	if err != nil {
		t.Error(err.Error())
	}
	t.Logf("CheckoutURL: %s\n", result.CheckoutURL)
}

func TestCurrencyAmountRoundTrip(t *testing.T) {
	for _, test := range []struct {
		currency               string
		amount, rounded, units int64
	}{
		{"USD", 1029, 1029, 1029}, {"JPY", 1050, 1100, 11}, {"JPY", 1049, 1000, 10},
		{"KWD", 1029, 1029, 10290}, {"BHD", 1, 1, 10},
	} {
		amount := RoundHundredths(test.amount, test.currency)
		units := ToStripeAmount(amount, test.currency)
		if amount != test.rounded || units != test.units || FromStripeAmount(units, test.currency) != amount {
			t.Fatalf("currency %s: rounded %d, units %d", test.currency, amount, units)
		}
	}
}

func TestCheckoutClientUsesInstanceKeyAndIdempotency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer account-key" {
			t.Error("checkout did not use the instance key")
		}
		if request.Header.Get("Idempotency-Key") != "checkout:order-1" {
			t.Error("checkout has no stable idempotency key")
		}
		if err := request.ParseForm(); err != nil {
			t.Error(err)
		}
		if request.Form.Get("line_items[0][price_data][unit_amount]") != "11" {
			t.Error("incorrect zero-decimal amount")
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"cs_test","url":"https://checkout.example/session"}`))
	}))
	defer server.Close()
	client := NewClient(Config{SecretKey: "account-key"})
	client.backend = stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{URL: stripe.String(server.URL), HTTPClient: server.Client(), MaxNetworkRetries: stripe.Int64(0)})
	globalKey := stripe.Key
	result, err := client.CreateCheckoutSessionContext(context.Background(), &Order{OrderNo: "order-1", Amount: 1050, Currency: "JPY", Payment: "card"}, &User{}, "https://site.example/paid", "https://site.example/canceled")
	if err != nil || result.TradeNo != "cs_test" {
		t.Fatalf("checkout failed: %v", err)
	}
	if stripe.Key != globalKey {
		t.Fatal("Stripe client changed the global account key")
	}
}

func TestCheckoutNotificationRequiresPaymentStatus(t *testing.T) {
	for _, eventType := range []string{"checkout.session.completed", "checkout.session.async_payment_succeeded"} {
		for _, paid := range []bool{false, true} {
			status := "unpaid"
			if paid {
				status = "paid"
			}
			payload, err := json.Marshal(map[string]any{
				"id": "evt_test", "object": "event", "type": eventType, "api_version": stripe.APIVersion,
				"data": map[string]any{"object": map[string]any{"id": "cs_test", "client_reference_id": "order", "payment_status": status, "amount_total": 11, "currency": "jpy"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			signature := fmt.Sprintf("t=%d,v1=%x", now.Unix(), webhook.ComputeSignature(now, payload, "signing-secret"))
			result, err := NewClient(Config{WebhookSecret: "signing-secret"}).ParseNotify(payload, signature)
			if err != nil || result.Paid != paid || result.Amount != 1100 || !strings.EqualFold(result.Currency, "JPY") {
				t.Fatalf("paid %t: notification %+v, %v", paid, result, err)
			}
		}
	}
}
