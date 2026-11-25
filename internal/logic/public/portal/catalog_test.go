package portal

import (
	"database/sql"
	"errors"
	"strconv"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	publicpayment "github.com/perfect-panel/server/internal/logic/public/payment"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	_ "modernc.org/sqlite"
)

func TestCatalogSelectsPublishedPlansAndFallsBackToDefaultLanguage(t *testing.T) {
	service, db := newCatalogFixture(t)
	ctx := t.Context()
	defaultPlan := db.Subscribe.Create().SetName("Monthly").SetShow(true).SetSort(2).SaveX(ctx)
	firstPlan := db.Subscribe.Create().SetName("Starter").SetShow(true).SetSort(1).SaveX(ctx)
	localized := db.Subscribe.Create().SetName("Mensuel").SetLanguage("fr").SetShow(true).
		SetDiscount(`[{"quantity":12,"discount":0.15}]`).SaveX(ctx)
	db.Subscribe.Create().SetName("Brouillon").SetLanguage("fr").SetShow(false).SaveX(ctx)
	db.Subscribe.Create().SetName("Entwurf").SetLanguage("de").SetShow(false).SaveX(ctx)

	logic := NewGetSubscriptionLogic(ctx, service)
	fr, err := logic.GetSubscription(&types.GetSubscriptionRequest{Language: "fr"})
	if err != nil || len(fr.List) != 1 || fr.List[0].Id != localized.ID {
		t.Fatalf("French catalog = %+v, %v", fr, err)
	}
	if got := fr.List[0].Discount; len(got) != 1 || got[0].Quantity != 12 || got[0].Discount != 0.15 {
		t.Fatalf("localized discounts = %+v", got)
	}
	for _, language := range []string{"", "de", "es"} {
		got, err := logic.GetSubscription(&types.GetSubscriptionRequest{Language: language})
		if err != nil || len(got.List) != 2 || got.List[0].Id != firstPlan.ID || got.List[1].Id != defaultPlan.ID {
			t.Fatalf("%q catalog = %+v, %v", language, got, err)
		}
	}
}

func TestCustomerPaymentCatalogsOnlyListEnabledMethodsInConfiguredOrder(t *testing.T) {
	service, db := newCatalogFixture(t)
	ctx := t.Context()
	second := db.Payment.Create().SetName("Bank transfer").SetPlatform("epay").SetConfig(`{}`).
		SetToken("bank-fixture").SetCurrencyUnit("EUR").SetSort(5).SetEnable(true).SaveX(ctx)
	first := db.Payment.Create().SetName("Card").SetPlatform("stripe").SetConfig(`{}`).
		SetToken("card-fixture").SetCurrencyUnit("USD").SetSort(1).SetEnable(true).SetFeeAmount(25).SaveX(ctx)
	db.Payment.Create().SetName("Draft").SetPlatform("stripe").SetConfig(`{}`).SetToken("draft-fixture").SetSort(0).SaveX(ctx)

	for name, getMethods := range map[string]func() (*types.GetAvailablePaymentMethodsResponse, error){
		"guest":    NewGetAvailablePaymentMethodsLogic(ctx, service).GetAvailablePaymentMethods,
		"customer": publicpayment.NewGetAvailablePaymentMethodsLogic(ctx, service).GetAvailablePaymentMethods,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := getMethods()
			if err != nil || len(got.List) != 2 || got.List[0].Id != first.ID || got.List[0].FeeAmount != 25 || got.List[1].Id != second.ID || got.List[1].CurrencyUnit != "EUR" {
				t.Fatalf("payment catalog = %+v, %v", got, err)
			}
		})
	}
}

func TestPurchasePreviewAppliesTermDiscountCouponAndPaymentFeeInOrder(t *testing.T) {
	service, db := newCatalogFixture(t)
	ctx := t.Context()
	plan := db.Subscribe.Create().SetName("Yearly option").SetUnitPrice(1000).
		SetDiscount(`[{"quantity":3,"discount":80},{"quantity":6,"discount":70}]`).SaveX(ctx)
	method := db.Payment.Create().SetPlatform("stripe").SetConfig(`{}`).SetFeeMode(3).
		SetFeePercent(10).SetFeeAmount(25).SaveX(ctx)
	at := time.Now()
	coupon := db.Coupon.Create().SetCode("TERM-TEST").SetType(2).SetDiscount(500).SetCount(1).
		SetStartTime(at.Add(-time.Hour).UnixMilli()).SetExpireTime(at.Add(time.Hour).UnixMilli()).
		SetSubscribe(strconv.FormatInt(plan.ID, 10)).SaveX(ctx)
	preview := NewPrePurchaseOrderLogic(ctx, service)
	req := &types.PrePurchaseOrderRequest{SubscribeId: plan.ID, Quantity: 6, Payment: method.ID, Coupon: coupon.Code}
	got, err := preview.PrePurchaseOrder(req)
	if err != nil || got.Price != 6000 || got.Discount != 1800 || got.CouponDiscount != 500 || got.FeeAmount != 395 || got.Amount != 4095 || got.Coupon != coupon.Code {
		t.Fatalf("purchase preview = %+v, %v", got, err)
	}
	if current := db.Coupon.GetX(ctx, coupon.ID); current.UsedCount != 0 || db.Order.Query().CountX(ctx) != 0 {
		t.Fatal("preview consumed the coupon or created an order")
	}
	db.Coupon.UpdateOneID(coupon.ID).SetUsedCount(1).SaveX(ctx)
	got, err = preview.PrePurchaseOrder(req)
	var code *xerr.CodeError
	if got != nil || !errors.As(err, &code) || code.GetErrCode() != xerr.CouponInsufficientUsage {
		t.Fatalf("preview with exhausted coupon = %+v, %v", got, err)
	}
}

func newCatalogFixture(t *testing.T) (*svc.ServiceContext, *ent.Client) {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	return &svc.ServiceContext{Store: repository.NewStore(client, nil)}, client
}
