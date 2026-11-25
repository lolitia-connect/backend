package subscription

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/model/entitlement"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tasklock"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func TestSubscriptionSweepFinishesEligibleSubscriptionsAndNotifiesOnce(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	addr := miniredis.RunT(t).Addr()
	cache := redis.NewClient(&redis.Options{Addr: addr})
	options := asynq.RedisClientOpt{Addr: addr, DB: 5}
	queueClient, inspector := asynq.NewClient(options), asynq.NewInspector(options)
	t.Cleanup(func() { _ = cache.Close(); _ = queueClient.Close(); _ = inspector.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, cache), Redis: cache, Queue: queueClient}
	service.Config.Site.SiteName = "Subscription panel"
	plan := client.Subscribe.Create().SetName("Metered plan").SaveX(ctx)
	now := time.Now().Truncate(time.Second)
	future, expired := now.Add(time.Hour), now.Add(-time.Hour)
	var expected []struct {
		id     int64
		status uint8
	}
	for _, tc := range []struct {
		name      string
		expiry    time.Time
		download  int64
		unlimited bool
		managed   bool
		want      uint8
	}{
		{"quota", future, 60, false, false, 2},
		{"expiry", expired, 59, false, false, 3},
		{"below-quota", future, 59, false, false, 1},
		{"unlimited", future, 100, true, false, 1},
		{"provider", expired, 100, false, true, 1},
		{"no-expiry", time.Unix(0, 0), 0, false, false, 1},
	} {
		account := client.User.Create().SetPassword("fixture").SaveX(ctx)
		client.UserAuthMethod.Create().SetUserID(account.ID).SetAuthType("email").SetAuthIdentifier(tc.name + "@example.test").SaveX(ctx)
		create := client.UserSubscribe.Create().SetUserID(account.ID).SetOrderID(account.ID).SetSubscribeID(plan.ID).
			SetStatus(1).SetTraffic(100).SetDownload(tc.download).SetUpload(40).SetExpireTime(tc.expiry).SetTrafficUnlimited(tc.unlimited)
		if tc.managed {
			create.SetEntitlementSource(entitlement.SourceApple)
		}
		id := create.SaveX(ctx).ID
		expected = append(expected, struct {
			id     int64
			status uint8
		}{id, tc.want})
	}
	logic := NewCheckSubscriptionLogic(service)
	task := asynq.NewTask(queue.SchedulerCheckSubscription, nil)
	lock, held, err := tasklock.Acquire(ctx, cache, "subscription:sweep:lock", time.Minute)
	if err != nil || !held {
		t.Fatalf("acquire competing sweep lock: held=%v, err=%v", held, err)
	}
	if err := logic.ProcessTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	for _, row := range expected {
		if got := client.UserSubscribe.GetX(ctx, row.id); got.Status != 1 || got.FinishedAt != nil {
			t.Fatalf("sweep updated subscription %d while another worker held the lock", row.id)
		}
	}
	if released, err := lock.Release(ctx); err != nil || !released {
		t.Fatalf("release competing sweep lock: released=%v, err=%v", released, err)
	}
	for range 2 {
		if err := logic.ProcessTask(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range expected {
		got := client.UserSubscribe.GetX(ctx, row.id)
		if got.Status != row.status || (got.FinishedAt != nil) != (row.status != 1) {
			t.Fatalf("subscription %d status=%d, finished=%v, want status=%d", row.id, got.Status, got.FinishedAt, row.status)
		}
	}
	pending, err := inspector.ListPendingTasks("default")
	if err != nil || len(pending) != 2 {
		t.Fatalf("expected two notifications after repeated sweeps, got %d, %v", len(pending), err)
	}
	want := map[string]string{"quota@example.test": queue.EmailTypeTrafficExceed, "expiry@example.test": queue.EmailTypeExpiration}
	for _, message := range pending {
		var payload queue.SendEmailPayload
		if err := json.Unmarshal(message.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		kind, found := want[payload.Email]
		if !found || message.Type != queue.ForthwithSendEmail || payload.Type != kind || payload.Content["SiteName"] != service.Config.Site.SiteName {
			t.Fatalf("unexpected notification: %+v", payload)
		}
		if kind == queue.EmailTypeExpiration && payload.Content["ExpireDate"] != expired.In(time.Local).Format("2006-01-02 15:04:05") {
			t.Fatalf("expiration date = %v, want %v", payload.Content["ExpireDate"], expired)
		}
		delete(want, payload.Email)
	}
}
