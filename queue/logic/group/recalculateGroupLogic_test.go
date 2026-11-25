package group

import (
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	queueTypes "github.com/perfect-panel/server/queue/types"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func TestQueuedGroupRecalculationHonorsTriggerAndSettings(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	logic := NewRecalculateGroupLogic(&svc.ServiceContext{Ent: client, Store: repository.NewStore(client, cache), Redis: cache})
	fromOrder := asynq.NewTask(queueTypes.OrderRecalculateGroup, nil)
	scheduled := asynq.NewTask(queueTypes.SchedulerRecalculateGroup, nil)
	if err := logic.ProcessTask(ctx, fromOrder); err != nil {
		t.Fatalf("order recalculation is optional without group settings: %v", err)
	}
	enabled := client.System.Create().SetCategory("group").SetKey("enabled").SetDesc("Enable grouping").SetValue("false").SaveX(ctx)
	mode := client.System.Create().SetCategory("group").SetKey("mode").SetDesc("Grouping mode").SetValue("average").SaveX(ctx)
	account := client.User.Create().SetPassword("fixture").SaveX(ctx)
	client.UserAuthMethod.Create().SetUserID(account.ID).SetAuthType("email").SetAuthIdentifier("member@example.test").SaveX(ctx)
	eligible := client.NodeGroup.Create().SetName("eligible").SetForCalculation(true).SetMinTrafficGB(0).SetMaxTrafficGB(20).SaveX(ctx)
	previous := client.NodeGroup.Create().SetName("manual").SetForCalculation(false).SaveX(ctx)
	plan := client.Subscribe.Create().SetNodeGroupIds([]int64{eligible.ID}).SaveX(ctx)
	subscription := client.UserSubscribe.Create().SetUserID(account.ID).SetOrderID(1).SetSubscribeID(plan.ID).
		SetNodeGroupID(previous.ID).SetStatus(1).SetTraffic(10 * 1024 * 1024 * 1024).SaveX(ctx)
	assertSkipped := func(task *asynq.Task) {
		t.Helper()
		if err := logic.ProcessTask(ctx, task); err != nil {
			t.Fatal(err)
		}
		if client.GroupHistory.Query().CountX(ctx) != 0 || client.UserSubscribe.GetX(ctx, subscription.ID).NodeGroupID != previous.ID {
			t.Fatalf("%s changed grouping despite the current configuration", task.Type())
		}
	}
	assertSkipped(fromOrder)
	client.System.UpdateOneID(enabled.ID).SetValue("true").SaveX(ctx)
	assertSkipped(scheduled)
	if err := logic.ProcessTask(ctx, fromOrder); err != nil {
		t.Fatal(err)
	}
	first := client.GroupHistory.Query().OnlyX(ctx)
	if first.State != "completed" || first.TriggerType != "manual" || first.GroupMode != "average" ||
		client.UserSubscribe.GetX(ctx, subscription.ID).NodeGroupID != eligible.ID {
		t.Fatalf("order-triggered average grouping did not persist the assignment: %+v", first)
	}

	client.System.UpdateOneID(mode.ID).SetValue("traffic").SaveX(ctx)
	client.UserSubscribe.UpdateOneID(subscription.ID).SetNodeGroupID(previous.ID).SaveX(ctx)
	if err := logic.ProcessTask(ctx, scheduled); err != nil {
		t.Fatal(err)
	}
	history := client.GroupHistory.Query().AllX(ctx)
	if len(history) != 2 || client.UserSubscribe.GetX(ctx, subscription.ID).NodeGroupID != eligible.ID {
		t.Fatalf("scheduled traffic grouping did not persist the assignment: %+v", history)
	}
	for _, run := range history {
		if run.ID != first.ID && (run.TriggerType != "scheduled" || run.GroupMode != "traffic" || run.State != "completed") {
			t.Fatalf("unexpected scheduled run: %+v", run)
		}
	}
}
