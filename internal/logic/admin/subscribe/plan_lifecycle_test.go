package subscribe

import (
	"database/sql"
	"errors"
	"slices"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	entplan "github.com/perfect-panel/server/ent/subscribe"
	publicsub "github.com/perfect-panel/server/internal/logic/public/subscribe"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/device"
	"github.com/perfect-panel/server/pkg/xerr"
	_ "modernc.org/sqlite"
)

func TestPlanCatalogUpdateAndDeletionWithActiveSubscriptions(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	manager := device.NewDeviceManager(60, 30)
	t.Cleanup(manager.Close)
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil), DeviceManager: manager}
	published, hidden := true, false
	client.Subscribe.Create().SetName("Archived").SetLanguage("en").SetShow(false).SetSell(false).SetSort(1).SaveX(ctx)
	create := NewCreateSubscribeLogic(ctx, service)
	discounts := []types.SubscribeDiscount{{Quantity: 3, Discount: 80}}
	if err := create.CreateSubscribe(&types.CreateSubscribeRequest{
		Name: "Monthly", Language: "en", UnitTime: "month", UnitPrice: 1200, Inventory: 5,
		Nodes: types.StringInt64Slice{10, 20}, NodeGroupIds: []string{"7", "9"}, Discount: discounts, Show: &published, Sell: &published,
	}); err != nil {
		t.Fatal(err)
	}
	plan := client.Subscribe.Query().Where(entplan.Name("Monthly")).OnlyX(ctx)
	if plan.Sort != 2 {
		t.Fatalf("new plan sort = %d, want 2", plan.Sort)
	}
	account := client.User.Create().SetPassword("fixture").SaveX(ctx)
	subscription := client.UserSubscribe.Create().SetUserID(account.ID).SetSubscribeID(plan.ID).SetOrderID(1).SetStatus(1).SaveX(ctx)
	client.UserSubscribe.Create().SetUserID(account.ID).SetSubscribeID(plan.ID).SetOrderID(2).SetStatus(3).SaveX(ctx)

	admin := NewGetSubscribeListLogic(ctx, service)
	filtered, err := admin.GetSubscribeList(&types.GetSubscribeListRequest{Page: 1, Size: 10, Language: "en", Search: "Monthly"})
	if err != nil || filtered.Total != 1 || len(filtered.List) != 1 || filtered.List[0].Id != plan.ID || filtered.List[0].Sold != 1 ||
		!slices.Equal(filtered.List[0].Nodes, types.StringInt64Slice{10, 20}) || !slices.Equal(filtered.List[0].Discount, discounts) {
		t.Fatalf("admin plan catalog = %+v, %v", filtered, err)
	}
	catalog := publicsub.NewQuerySubscribeListLogic(ctx, service)
	before, err := catalog.QuerySubscribeList(&types.QuerySubscribeListRequest{Language: "en"})
	if err != nil || before.Total != 1 || len(before.List) != 1 || before.List[0].Id != plan.ID {
		t.Fatalf("customer plan catalog = %+v, %v", before, err)
	}

	if err := NewUpdateSubscribeLogic(ctx, service).UpdateSubscribe(&types.UpdateSubscribeRequest{
		Id: plan.ID, Name: "Monthly archive", Language: "en", UnitTime: "month", UnitPrice: 1500, Inventory: 3, Sort: plan.Sort,
		Nodes: types.StringInt64Slice{10, 20}, NodeTags: []string{"regional"}, NodeGroupIds: []string{"9"}, Discount: discounts,
		Show: &hidden, Sell: &hidden,
	}); err != nil {
		t.Fatal(err)
	}
	details := NewGetSubscribeDetailsLogic(ctx, service)
	updated, err := details.GetSubscribeDetails(&types.GetSubscribeDetailsRequest{Id: plan.ID})
	if err != nil || updated.UnitPrice != 1500 || updated.Inventory != 3 || updated.Sell || updated.Show || len(updated.Nodes) != 0 ||
		!slices.Equal(updated.NodeTags, []string{"regional"}) || !slices.Equal(updated.NodeGroupIds, []string{"9"}) {
		t.Fatalf("updated plan details = %+v, %v", updated, err)
	}
	after, err := catalog.QuerySubscribeList(&types.QuerySubscribeListRequest{Language: "en"})
	if err != nil || after.Total != 0 || len(after.List) != 0 {
		t.Fatalf("archived plan is still on sale: %+v, %v", after, err)
	}

	remove := NewDeleteSubscribeLogic(ctx, service)
	request := &types.DeleteSubscribeRequest{Id: plan.ID}
	var problem *xerr.CodeError
	if err := remove.DeleteSubscribe(request); !errors.As(err, &problem) || problem.GetErrCode() != xerr.SubscribeIsUsedError {
		t.Fatalf("deleting a plan with active subscribers returned %v", err)
	}
	if kept := client.Subscribe.GetX(ctx, plan.ID); kept.Name != "Monthly archive" {
		t.Fatal("rejected deletion affected plan")
	}
	client.UserSubscribe.UpdateOneID(subscription.ID).SetStatus(3).SaveX(ctx)
	if err := remove.DeleteSubscribe(request); err != nil {
		t.Fatal(err)
	}
	if kept := client.Subscribe.Query().OnlyX(ctx); kept.Name != "Archived" || kept.Sort != 1 {
		t.Fatalf("remaining plan and its sort position = %+v", kept)
	}
}
