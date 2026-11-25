package portal

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/redis/go-redis/v9"
	stripego "github.com/stripe/stripe-go/v81"
	_ "modernc.org/sqlite"
)

type stripeCheckoutTransport func(*http.Request) (*http.Response, error)

func (transport stripeCheckoutTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestStripeCheckoutPersistsPaymentExpectationAndReturnURLs(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, cache), Redis: cache}
	service.Config.Currency.Unit = "USD"
	service.Config.Site.Host = "store.example.test"
	method := client.Payment.Create().SetPlatform("Stripe").SetEnable(true).SetCurrencyUnit("USD").
		SetConfig(`{"secret_key":"fixture-secret","payment":"card"}`).SaveX(ctx)
	invoice := client.Order.Create().SetOrderNo("stripe-checkout").SetUserID(7).SetPaymentID(method.ID).
		SetMethod(method.Platform).SetAmount(1250).SetStatus(order.StatusPending).SetStateVersion(1).SaveX(ctx)

	var requests int
	backend := stripego.GetBackend(stripego.APIBackend)
	t.Cleanup(func() { stripego.SetBackend(stripego.APIBackend, backend) })
	transport := stripeCheckoutTransport(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.Method != http.MethodPost || request.URL.Path != "/v1/checkout/sessions" {
			return nil, fmt.Errorf("unexpected Stripe request %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer fixture-secret" || request.Header.Get("Idempotency-Key") != "checkout:"+invoice.OrderNo {
			return nil, fmt.Errorf("Stripe request did not carry the checkout credentials")
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, err
		}
		if values.Get("line_items[0][price_data][unit_amount]") != "1250" || values.Get("line_items[0][price_data][currency]") != "usd" ||
			values.Get("line_items[0][price_data][product_data][name]") != "Order "+invoice.OrderNo ||
			values.Get("success_url") != "https://store.example.test/return?order_no=stripe-checkout&stripe_return=1" ||
			values.Get("cancel_url") != "https://store.example.test/return?order_no=stripe-checkout" ||
			values.Get("metadata[order_no]") != invoice.OrderNo || values.Get("metadata[user_id]") != "7" {
			return nil, fmt.Errorf("unexpected Stripe checkout fields: %s", values.Encode())
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"cs_fixture","url":"https://checkout.example/session"}`))}, nil
	})
	stripego.SetBackend(stripego.APIBackend, stripego.GetBackendWithConfig(stripego.APIBackend, &stripego.BackendConfig{
		HTTPClient: &http.Client{Transport: transport}, MaxNetworkRetries: stripego.Int64(0),
	}))

	response, err := NewPurchaseCheckoutLogic(ctx, service).PurchaseCheckout(&types.CheckoutOrderRequest{
		OrderNo: invoice.OrderNo, ReturnUrl: "https://store.example.test/return",
	})
	if err != nil || response == nil || response.Type != "url" || response.CheckoutUrl != "https://checkout.example/session" {
		t.Fatalf("Stripe checkout = %+v, %v", response, err)
	}
	stored := client.Order.GetX(ctx, invoice.ID)
	if stored.TradeNo == nil || *stored.TradeNo != "cs_fixture" || stored.PaymentAmount != 1250 || stored.PaymentCurrency != "USD" || requests != 1 {
		t.Fatalf("stored Stripe checkout = %+v, requests=%d", stored, requests)
	}
	invalid := client.Order.Create().SetOrderNo("stripe-invalid-return").SetUserID(7).SetPaymentID(method.ID).
		SetMethod(method.Platform).SetAmount(1250).SetStatus(order.StatusPending).SetStateVersion(1).SaveX(ctx)
	if _, err := NewPurchaseCheckoutLogic(ctx, service).PurchaseCheckout(&types.CheckoutOrderRequest{
		OrderNo: invalid.OrderNo, ReturnUrl: "javascript:invalid",
	}); err == nil || !strings.Contains(err.Error(), "invalid Stripe return URL") {
		t.Fatalf("invalid Stripe return URL error = %v", err)
	}
}
