package coupon

import (
	"database/sql"
	"slices"
	"strconv"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	entcoupon "github.com/perfect-panel/server/ent/coupon"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	_ "modernc.org/sqlite"
)

func TestCouponAdministrationMaintainsPlanScopeAndUsage(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil)}
	create := NewCreateCouponLogic(ctx, service)
	now := time.Now()
	fixture := types.CreateCouponRequest{Name: "Welcome offer", Type: 1, Count: 3, Discount: 10,
		StartTime: now.Add(-time.Hour).Unix(), ExpireTime: now.Add(time.Hour).Unix(), Subscribe: []string{"10", "2", "9"}}
	if err := create.CreateCoupon(&fixture); err != nil {
		t.Fatal(err)
	}
	first := client.Coupon.Query().OnlyX(ctx)
	if fixture.Code == "" || first.Code != fixture.Code || !first.Enable || first.UsedCount != 0 {
		t.Fatalf("new coupon was not published: %+v", first)
	}
	other := fixture
	other.Code, other.Subscribe = "WELCOME-20", []string{"20"}
	if err := create.CreateCoupon(&other); err != nil {
		t.Fatal(err)
	}
	if err := create.CreateCoupon(&other); err == nil || client.Coupon.Query().CountX(ctx) != 2 {
		t.Fatal("duplicate coupon code created a second record")
	}
	list := NewGetCouponListLogic(ctx, service)
	result, err := list.GetCouponList(&types.GetCouponListRequest{Page: 1, Size: 1, Search: "Welcome", Subscribe: 2})
	if err != nil || result.Total != 1 || len(result.List) != 1 || result.List[0].Id != first.ID ||
		!slices.Equal(result.List[0].Subscribe, fixture.Subscribe) {
		t.Fatalf("coupon search for plan 2 = %+v, %v", result, err)
	}
	store := service.Store.Coupon()
	if err := store.ReserveUsage(ctx, fixture.Code); err != nil {
		t.Fatal(err)
	}
	update := NewUpdateCouponLogic(ctx, service)
	request := &types.UpdateCouponRequest{Id: first.ID, Name: "Paused offer", Code: first.Code, Type: first.Type,
		Count: first.Count, Discount: 15, StartTime: first.StartTime, ExpireTime: first.ExpireTime, Subscribe: []string{"9"}, UsedCount: 99}
	if err := update.UpdateCoupon(request); err == nil || !client.Coupon.GetX(ctx, first.ID).Enable {
		t.Fatal("update with no enable flag changed the coupon")
	}
	disabled := false
	request.Enable = &disabled
	if err := update.UpdateCoupon(request); err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveUsage(ctx, first.Code); err == nil {
		t.Fatal("disabled coupon was still redeemable")
	}
	updated := client.Coupon.GetX(ctx, first.ID)
	if updated.Enable || updated.UsedCount != 1 || updated.Discount != 15 || updated.Subscribe != "9" {
		t.Fatalf("updated coupon = %+v", updated)
	}
	second := client.Coupon.Query().Where(entcoupon.Code(other.Code)).OnlyX(ctx)
	if err := NewDeleteCouponLogic(ctx, service).DeleteCoupon(&types.DeleteCouponRequest{Id: first.ID}); err != nil {
		t.Fatal(err)
	}
	if remaining := client.Coupon.Query().OnlyX(ctx); remaining.ID != second.ID {
		t.Fatalf("delete retained the wrong coupon: %+v", remaining)
	}
	if err := NewBatchDeleteCouponLogic(ctx, service).BatchDeleteCoupon(&types.BatchDeleteCouponRequest{
		Ids: []string{strconv.FormatInt(first.ID, 10), strconv.FormatInt(second.ID, 10)},
	}); err != nil || client.Coupon.Query().CountX(ctx) != 0 {
		t.Fatalf("batch delete did not remove the remaining coupon: %v", err)
	}
}
