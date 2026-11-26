package task

import (
	"errors"
	"net/url"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/logic/public/portal"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/types"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRateRefreshPreservesLockedCheckoutAmounts(t *testing.T) {
	service, db := quotaFixture(t)
	ctx := t.Context()
	service.Redis = redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = service.Redis.Close() })
	service.Config.Currency.Unit = "USD"
	service.Config.Site.Host = "shop.example.test"
	service.Config.Site.SiteName = "Test store"
	db.System.Create().SetCategory("currency").SetKey("CurrencyUnit").SetType("string").SetValue("USD").SetDesc("Billing currency").SaveX(ctx)
	worker := NewRateLogic(service)
	rate, calls := 2.5, 0
	var providerErr error
	worker.quote = func(from, to, access string, amount float64) (float64, error) {
		calls++
		require.Equal(t, "USD", from)
		require.Equal(t, "CNY", to)
		require.Equal(t, "test-provider", access)
		require.Equal(t, 1.0, amount)
		return rate, providerErr
	}
	require.NoError(t, worker.ProcessTask(ctx, nil))
	require.Zero(t, calls)
	db.System.Create().SetCategory("currency").SetKey("AccessKey").SetType("string").SetValue("test-provider").SetDesc("Rate provider key").SaveX(ctx)
	require.NoError(t, worker.ProcessTask(ctx, nil))
	method := db.Payment.Create().SetEnable(true).SetPlatform("EPay").SetCurrencyUnit("").SetToken("cashier").
		SetConfig(`{"pid":"1001","key":"local","url":"https://cashier.example.test","type":"alipay"}`).SaveX(ctx)
	invoice := func(number string) *ent.Order {
		return db.Order.Create().SetOrderNo(number).SetPaymentID(method.ID).SetMethod(method.Platform).
			SetAmount(1234).SetStatus(order.StatusPending).SaveX(ctx)
	}
	checkout := func(inv *ent.Order, wantMoney string, cents int64) {
		t.Helper()
		result, err := portal.NewPurchaseCheckoutLogic(ctx, service).PurchaseCheckout(&types.CheckoutOrderRequest{OrderNo: inv.OrderNo, ReturnUrl: "https://shop.example.test/receipt"})
		require.NoError(t, err)
		redirect, err := url.Parse(result.CheckoutUrl)
		require.NoError(t, err)
		require.Equal(t, inv.OrderNo, redirect.Query().Get("out_trade_no"))
		require.Equal(t, wantMoney, redirect.Query().Get("money"))
		stored := db.Order.GetX(ctx, inv.ID)
		require.Equal(t, "CNY", stored.PaymentCurrency)
		require.Equal(t, cents, stored.PaymentAmount)
		require.Equal(t, inv.Amount, stored.Amount)
	}
	providerErr = errors.New("rate feed unavailable")
	rate = 0
	require.ErrorIs(t, worker.ProcessTask(ctx, nil), providerErr)
	first := invoice("before-refresh")
	checkout(first, "30.85", 3085)
	providerErr = nil
	rate = 3
	require.NoError(t, worker.ProcessTask(ctx, nil))
	checkout(first, "30.85", 3085)
	checkout(invoice("after-refresh"), "37.02", 3702)

	// A USD lookup is outstanding when the operator switches to EUR and refreshes it.
	worker.quote = func(from, to, _ string, _ float64) (float64, error) {
		require.Equal(t, "USD", from)
		require.Equal(t, "CNY", to)
		require.NoError(t, service.Store.System().UpdateValueByCategoryKey(ctx, "currency", "CurrencyUnit", "EUR"))
		service.Config.Currency.Unit = "EUR"
		newWorker := NewRateLogic(service)
		newWorker.quote = func(from, to, _ string, _ float64) (float64, error) {
			require.Equal(t, "EUR", from)
			require.Equal(t, "CNY", to)
			return 4, nil
		}
		require.NoError(t, newWorker.ProcessTask(ctx, nil))
		return 3, nil
	}
	require.NoError(t, worker.ProcessTask(ctx, nil))
	checkout(first, "30.85", 3085)
	checkout(invoice("euro-invoice"), "49.36", 4936)
}
