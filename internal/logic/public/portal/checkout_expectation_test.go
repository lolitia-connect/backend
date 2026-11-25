package portal

import (
	"database/sql"
	"net/url"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/ent/orderevent"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func TestEPayPageCheckoutReusesPersistedAmountAfterRateChange(t *testing.T) {
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
	service.Config.Site.SiteName = "Test Store"
	service.Config.Site.Host = "store.example.test"

	method := client.Payment.Create().SetPlatform("EPay").SetEnable(false).SetCurrencyUnit("CNY").SetExchangeRate(2.5).
		SetConfig(`{"pid":"1001","key":"fixture-key","url":"https://cashier.example.test/shop","type":"alipay"}`).SaveX(ctx)
	invoice := client.Order.Create().SetOrderNo("page-checkout").SetPaymentID(method.ID).SetMethod(method.Platform).
		SetAmount(1234).SetStatus(order.StatusPending).SetStateVersion(1).SaveX(ctx)
	request := &types.CheckoutOrderRequest{OrderNo: invoice.OrderNo, ReturnUrl: "https://store.example.test/return"}

	if response, err := NewPurchaseCheckoutLogic(ctx, service).PurchaseCheckout(request); err == nil || response != nil {
		t.Fatalf("disabled checkout = %+v, %v", response, err)
	}
	if got := client.Order.GetX(ctx, invoice.ID); got.PaymentCurrency != "" || got.PaymentAmount != 0 {
		t.Fatalf("disabled checkout recorded a payment expectation: %+v", got)
	}

	client.Payment.UpdateOneID(method.ID).SetEnable(true).SaveX(ctx)
	for _, rate := range []float64{2.5, 3} {
		client.Payment.UpdateOneID(method.ID).SetExchangeRate(rate).SaveX(ctx)
		response, err := NewPurchaseCheckoutLogic(ctx, service).PurchaseCheckout(request)
		if err != nil || response == nil || response.Type != "url" {
			t.Fatalf("checkout at exchange rate %.1f = %+v, %v", rate, response, err)
		}
		redirect, err := url.Parse(response.CheckoutUrl)
		if err != nil {
			t.Fatal(err)
		}
		query := redirect.Query()
		if redirect.Host != "cashier.example.test" || query.Get("money") != "30.85" || query.Get("out_trade_no") != invoice.OrderNo {
			t.Fatalf("redirect at exchange rate %.1f = %s, want original payment amount for %s", rate, redirect, invoice.OrderNo)
		}
		got := client.Order.GetX(ctx, invoice.ID)
		if got.PaymentCurrency != "CNY" || got.PaymentAmount != 3085 || got.Amount != 1234 || got.Status != order.StatusPending || got.StateVersion != invoice.StateVersion ||
			got.TradeNo != nil || got.PaymentSession != nil {
			t.Fatalf("persisted checkout at exchange rate %.1f = %+v", rate, got)
		}
	}
	if got := client.OrderEvent.Query().Where(orderevent.OrderIDEQ(invoice.ID)).CountX(ctx); got != 0 {
		t.Fatalf("page checkout emitted %d order state events", got)
	}
}
