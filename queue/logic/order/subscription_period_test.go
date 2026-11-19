package orderLogic

import (
	"context"
	stdsql "database/sql"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/model/entitlement"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	_ "modernc.org/sqlite"
)

func TestRecordLocalSubscriptionPeriods(t *testing.T) {
	database, err := stdsql.Open("sqlite", "file:local-period-test?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })

	logic := &ActivateOrderLogic{svc: &svc.ServiceContext{Ent: client}}
	now := time.Now().UTC().Truncate(time.Millisecond)
	subscription := &user.Subscribe{
		Id:         17,
		StartTime:  now.Add(-time.Hour),
		ExpireTime: now.Add(30 * 24 * time.Hour),
		Traffic:    100,
	}
	purchase := &order.Order{Id: 10, OrderNo: "local-purchase", SubscribeId: 3, Type: OrderTypeSubscribe}
	if err := logic.recordLocalSubscriptionPeriod(context.Background(), purchase, subscription, false); err != nil {
		t.Fatalf("record purchase period: %v", err)
	}

	renewalStart := subscription.ExpireTime
	renewal := &order.Order{Id: 11, OrderNo: "local-renewal", SubscribeId: 4, Type: OrderTypeRenewal}
	subscription.ExpireTime = renewalStart.Add(31 * 24 * time.Hour)
	if err := logic.recordLocalSubscriptionPeriod(context.Background(), renewal, subscription, true, renewalStart); err != nil {
		t.Fatalf("record renewal period: %v", err)
	}

	first, err := client.SubscriptionPeriod.Get(context.Background(), entitlement.LocalPeriodID(purchase.OrderNo))
	if err != nil {
		t.Fatal(err)
	}
	if first.EntitlementID != entitlement.LocalEntitlementID(subscription.Id) || first.OrderID != purchase.Id || first.PlanID != purchase.SubscribeId || first.TrafficReset {
		t.Fatalf("unexpected purchase period: %+v", first)
	}
	if !first.StartAt.Equal(subscription.StartTime) || !first.EndAt.Equal(renewalStart) {
		t.Fatalf("purchase period bounds = %v..%v, want %v..%v", first.StartAt, first.EndAt, subscription.StartTime, renewalStart)
	}

	second, err := client.SubscriptionPeriod.Get(context.Background(), entitlement.LocalPeriodID(renewal.OrderNo))
	if err != nil {
		t.Fatal(err)
	}
	if second.OrderID != renewal.Id || second.PlanID != renewal.SubscribeId || !second.TrafficReset || !second.StartAt.Equal(renewalStart) || !second.EndAt.Equal(subscription.ExpireTime) {
		t.Fatalf("unexpected renewal period: %+v", second)
	}
}
