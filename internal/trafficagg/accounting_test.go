package trafficagg

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/repository"
	_ "modernc.org/sqlite"
)

var fixtureID atomic.Uint64

func accountingStore(t *testing.T) repository.Store {
	t.Helper()
	database, err := sql.Open("sqlite", fmt.Sprintf("file:traffic-%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", fixtureID.Add(1)))
	if err != nil {
		t.Fatal(err)
	}
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	return repository.NewStore(client, nil)
}

func TestBatchReplayAndOverflowRollback(t *testing.T) {
	store := accountingStore(t)
	ctx := context.Background()
	client := store.Ent()
	client.UserSubscribe.Create().SetID(1).SetUserID(7).SetOrderID(1).SetSubscribeID(3).SetStatus(1).SaveX(ctx)
	client.UserSubscribe.Create().SetID(2).SetUserID(8).SetOrderID(2).SetSubscribeID(3).SetStatus(3).SaveX(ctx)
	deltas := []Delta{{ServerID: 4, SubscriptionID: 1, Upload: 10, Download: 20}, {ServerID: 4, SubscriptionID: 2, Upload: 30, Download: 40}}
	for attempt := 0; attempt < 2; attempt++ {
		if err := ApplyBatch(ctx, store, "minute-1", time.Now(), deltas); err != nil {
			t.Fatal(err)
		}
	}
	active, expired := client.UserSubscribe.GetX(ctx, 1), client.UserSubscribe.GetX(ctx, 2)
	if active.Upload != 10 || active.Download != 20 || expired.ExpiredUpload != 30 || expired.ExpiredDownload != 40 || expired.Upload != 0 || client.TrafficLog.Query().CountX(ctx) != 2 {
		t.Fatalf("traffic replay changed accounting: active=%+v expired=%+v", active, expired)
	}
	client.UserSubscribe.UpdateOneID(2).SetExpiredUpload(math.MaxInt64).ExecX(ctx)
	if err := ApplyBatch(ctx, store, "minute-2", time.Now(), deltas); err == nil {
		t.Fatal("overflow batch accepted")
	}
	if client.UserSubscribe.GetX(ctx, 1).Upload != 10 || client.TrafficBatch.Query().CountX(ctx) != 1 || client.TrafficLog.Query().CountX(ctx) != 2 {
		t.Fatal("failed batch partially committed")
	}
}

func TestTrafficLogFailureRollsBackUsageAndMarker(t *testing.T) {
	store := accountingStore(t)
	ctx := context.Background()
	client := store.Ent()
	client.UserSubscribe.Create().SetID(1).SetUserID(7).SetOrderID(1).SetSubscribeID(3).SaveX(ctx)
	if err := client.Driver().Exec(ctx, "DROP TABLE traffic_log", []any{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := ApplyBatch(ctx, store, "failing-log", time.Now(), []Delta{{ServerID: 1, SubscriptionID: 1, Upload: 10}}); err == nil {
		t.Fatal("missing log table accepted")
	}
	if client.UserSubscribe.GetX(ctx, 1).Upload != 0 || client.TrafficBatch.Query().CountX(ctx) != 0 {
		t.Fatal("log failure left committed usage or marker")
	}
}
