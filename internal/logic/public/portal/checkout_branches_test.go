package portal

import (
	"database/sql"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/redis/go-redis/v9"
)

func newCheckoutBranchService(t *testing.T) *svc.ServiceContext {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	service := &svc.ServiceContext{Ent: client, Store: repository.NewStore(client, cache), Redis: cache}
	service.Config.Currency.Unit = "USD"
	service.Config.Site.Host = "store.example.test"
	service.Config.Site.SiteName = "Fixture Store"
	return service
}

func TestPurchaseCheckoutRejectsInvalidGatewayConfigurations(t *testing.T) {
	service := newCheckoutBranchService(t)
	ctx := t.Context()
	for _, platform := range []string{"Stripe", "AlipayF2F", "AlipayPlus", "CryptoSaaS", "Cryptomus", "EPay"} {
		t.Run(platform, func(t *testing.T) {
			method := service.Ent.Payment.Create().SetPlatform(platform).SetToken(strings.ToLower(platform)).SetEnable(true).SetConfig("not-json").SaveX(ctx)
			invoice := service.Ent.Order.Create().SetOrderNo("invalid-" + strings.ToLower(platform)).SetPaymentID(method.ID).
				SetMethod(platform).SetAmount(100).SetStatus(order.StatusPending).SetStateVersion(1).SaveX(ctx)
			response, err := NewPurchaseCheckoutLogic(ctx, service).PurchaseCheckout(&types.CheckoutOrderRequest{OrderNo: invoice.OrderNo})
			if err == nil || response != nil {
				t.Fatalf("invalid %s checkout = %+v, %v", platform, response, err)
			}
			if current := service.Ent.Order.GetX(ctx, invoice.ID); current.Status != order.StatusPending || current.TradeNo != nil || current.PaymentSession != nil {
				t.Fatalf("invalid %s checkout changed order = %+v", platform, current)
			}
		})
	}

	missingCurrency := service.Ent.Payment.Create().SetPlatform("AlipayPlus").SetToken("alipayplus-missing-currency").SetEnable(true).SetCurrencyUnit("").SetConfig(`{"payment_method":"ONLINE","gateway_url":"https://gateway.example.test"}`).SaveX(ctx)
	missingCurrencyOrder := service.Ent.Order.Create().SetOrderNo("alipayplus-missing-currency").SetPaymentID(missingCurrency.ID).
		SetMethod("AlipayPlus").SetAmount(100).SetStatus(order.StatusPending).SaveX(ctx)
	if _, err := NewPurchaseCheckoutLogic(ctx, service).PurchaseCheckout(&types.CheckoutOrderRequest{OrderNo: missingCurrencyOrder.OrderNo}); err == nil || !strings.Contains(err.Error(), "currency is empty") {
		t.Fatalf("missing AlipayPlus currency error = %v", err)
	}

	invalidAmountMethod := service.Ent.Payment.Create().SetPlatform("Cryptomus").SetToken("cryptomus-zero").SetEnable(true).SetConfig(`{"merchant_id":"merchant","api_key":"secret"}`).SaveX(ctx)
	invalidAmountOrder := service.Ent.Order.Create().SetOrderNo("cryptomus-zero").SetPaymentID(invalidAmountMethod.ID).
		SetMethod("Cryptomus").SetAmount(0).SetStatus(order.StatusPending).SaveX(ctx)
	if _, err := NewPurchaseCheckoutLogic(ctx, service).PurchaseCheckout(&types.CheckoutOrderRequest{OrderNo: invalidAmountOrder.OrderNo}); err == nil || !strings.Contains(err.Error(), "payment amount must be positive") {
		t.Fatalf("zero Cryptomus amount error = %v", err)
	}
}

func TestPurchaseCheckoutExchangeRateSelection(t *testing.T) {
	service := newCheckoutBranchService(t)
	logic := NewPurchaseCheckoutLogic(t.Context(), service)
	if amount, err := logic.queryExchangeRate("USD", 1250); err != nil || amount != 12.5 {
		t.Fatalf("same-currency conversion = %v, %v", amount, err)
	}
	service.ExchangeRate = 7.2
	if amount, err := logic.queryExchangeRate("CNY", 1250); err != nil || amount != 90 {
		t.Fatalf("configured conversion = %v, %v", amount, err)
	}
	service.ExchangeRate = 0
	if amount, err := logic.queryExchangeRate("EUR", 1250); err != nil || amount != 12.5 {
		t.Fatalf("no-key conversion fallback = %v, %v", amount, err)
	}
	method := &payment.Payment{CurrencyUnit: "CNY", ExchangeRate: 7.2}
	if amount, err := logic.queryPaymentMethodExchangeRate(method, 1250, "CNY"); err != nil || amount != 90 {
		t.Fatalf("payment-specific conversion = %v, %v", amount, err)
	}
	method = &payment.Payment{CurrencyUnit: "USD", ExchangeRate: 1}
	if amount, err := logic.queryPaymentMethodExchangeRate(method, 1250, "CNY"); err != nil || amount != 12.5 {
		t.Fatalf("payment same-currency conversion = %v, %v", amount, err)
	}
}
