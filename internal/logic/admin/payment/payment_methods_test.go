package payment

import (
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	_ "modernc.org/sqlite"
)

func TestPaymentMethodAdministrationLifecycle(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil)}
	service.Config.Currency.Unit = "CNY"
	ctx = t.Context()
	create := NewCreatePaymentMethodLogic(ctx, service)
	enabled := true
	request := &types.CreatePaymentMethodRequest{
		Name: "Alipay checkout", Platform: "AlipayF2F", Domain: "https://pay.example/",
		Config: map[string]interface{}{"app_id": "merchant-1", "sandbox": "true"},
		Enable: &enabled, CurrencyUnit: "cny", FeeAmount: 25, BillDesc: "Order {order_no}",
	}
	created, err := create.CreatePaymentMethod(request)
	if err != nil {
		t.Fatal(err)
	}
	stored := client.Payment.GetX(ctx, created.Id)
	config, ok := created.Config.(map[string]interface{})
	if !ok || created.Id == 0 || stored.Token == "" || created.CurrencyUnit != "CNY" || created.Sort != 1 || config["sandbox"] != true {
		t.Fatalf("created payment method = %+v, stored=%+v", created, stored)
	}
	if _, err := create.CreatePaymentMethod(&types.CreatePaymentMethodRequest{Platform: "unsupported"}); err == nil {
		t.Fatal("unsupported payment platform was accepted")
	}
	if _, err := create.CreatePaymentMethod(&types.CreatePaymentMethodRequest{Platform: "AlipayF2F", Config: "not-json"}); err == nil {
		t.Fatal("malformed platform configuration was accepted")
	}
	second, err := create.CreatePaymentMethod(&types.CreatePaymentMethodRequest{
		Name: "Disabled test method", Platform: "AlipayF2F", Config: map[string]interface{}{}, Enable: ptr(false),
	})
	if err != nil || second.Sort != created.Sort+1 {
		t.Fatalf("second payment method = %+v, %v", second, err)
	}

	list, err := NewGetPaymentMethodListLogic(ctx, service).GetPaymentMethodList(&types.GetPaymentMethodListRequest{
		Page: 1, Size: 10, Platform: "AlipayF2F", Search: "Alipay", Enable: &enabled,
	})
	if err != nil || list.Total != 1 || len(list.List) != 1 || list.List[0].Id != created.Id || list.List[0].NotifyURL != "https://pay.example/v1/notify/AlipayF2F/"+stored.Token {
		t.Fatalf("filtered payment methods = %+v, %v", list, err)
	}
	platforms, err := NewGetPaymentPlatformLogic(ctx, service).GetPaymentPlatform()
	if err != nil || len(platforms.List) == 0 {
		t.Fatalf("supported platforms = %+v, %v", platforms, err)
	}

	disabled := false
	updated, err := NewUpdatePaymentMethodLogic(ctx, service).UpdatePaymentMethod(&types.UpdatePaymentMethodRequest{
		Id: created.Id, Name: "Alipay primary", Platform: "AlipayF2F", Domain: "https://new-pay.example",
		Config: map[string]interface{}{"app_id": "merchant-2", "sandbox": false}, Enable: &disabled, FeeAmount: 40, CurrencyUnit: "cny",
	})
	if err != nil || updated.Enable == nil || *updated.Enable || updated.Sort != created.Sort || updated.CurrencyUnit != "CNY" || updated.FeeAmount != 40 {
		t.Fatalf("updated payment method = %+v, %v", updated, err)
	}
	platformChange := &types.UpdatePaymentMethodRequest{
		Id: created.Id, Name: "Not allowed", Platform: "Stripe", Config: map[string]interface{}{}, Enable: &enabled,
	}
	if _, err := NewUpdatePaymentMethodLogic(ctx, service).UpdatePaymentMethod(platformChange); err == nil {
		t.Fatal("payment method platform change was accepted")
	}
	if err := NewDeletePaymentMethodLogic(ctx, service).DeletePaymentMethod(&types.DeletePaymentMethodRequest{Id: created.Id}); err != nil {
		t.Fatal(err)
	}
	remaining, err := NewGetPaymentMethodListLogic(ctx, service).GetPaymentMethodList(&types.GetPaymentMethodListRequest{Page: 1, Size: 10})
	if err != nil || remaining.Total != 1 || remaining.List[0].Id != second.Id {
		t.Fatalf("methods after deletion = %+v, %v", remaining, err)
	}
	if err := service.Store.Payment().Delete(ctx, -1); err == nil {
		t.Fatal("default payment method was deletable")
	}
}

func ptr(value bool) *bool { return &value }
