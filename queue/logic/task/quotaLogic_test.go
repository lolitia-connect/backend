package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	entsystemlog "github.com/perfect-panel/server/ent/systemlog"
	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/task"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	_ "modernc.org/sqlite"
)

var quotaFixtureID atomic.Uint64

// quotaFixture builds an in-memory SQLite store plus the ServiceContext the
// quota logic needs. SQLite is the dialect the tests run on and has no
// `SELECT ... FOR UPDATE`; the model omits the clause there, so the locked
// re-read in processSubscription still exercises its real code path.
func quotaFixture(t *testing.T) (*svc.ServiceContext, *ent.Client) {
	t.Helper()
	database, err := sql.Open("sqlite", fmt.Sprintf("file:quota-task-%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", quotaFixtureID.Add(1)))
	if err != nil {
		t.Fatal(err)
	}
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	return &svc.ServiceContext{Store: repository.NewStore(client, nil)}, client
}

// seedTask inserts the task row processSubscribes records its outcome on.
func seedTask(t *testing.T, client *ent.Client, id int64) *task.Task {
	t.Helper()
	client.Task.Create().SetID(id).SetType(int8(task.TypeQuota)).SetStatus(0).SetTotal(1).SaveX(context.Background())
	return &task.Task{Id: id, Total: 1}
}

// seedSubscription inserts one subscription. base is time.Now() so the logic's
// own clock (processSubscribes uses time.Now()) sees a future expiry.
func seedSubscription(t *testing.T, client *ent.Client, id int64, edit func(*ent.UserSubscribeCreate)) time.Time {
	t.Helper()
	base := time.Now()
	create := client.UserSubscribe.Create().
		SetID(id).SetUserID(7).SetSubscribeID(9).SetOrderID(1).
		SetStatus(1).SetUpload(100).SetDownload(40).
		SetToken("T1").SetUUID("U1").
		SetExpireTime(base.Add(24 * time.Hour))
	if edit != nil {
		edit(create)
	}
	create.SaveX(context.Background())
	return base
}

// TestQuotaGrantKeepsConcurrentTrafficAndCredentials is the regression test for
// the whole-row clobber. The quota task snapshots its subscribers before it
// opens its transaction; a traffic settlement and a credential rotation landing
// in that window must both survive the grant, and only the grant's own columns
// may change.
func TestQuotaGrantKeepsConcurrentTrafficAndCredentials(t *testing.T) {
	service, client := quotaFixture(t)
	ctx := context.Background()
	base := seedSubscription(t, client, 1, nil)
	info := seedTask(t, client, 1)

	logic := NewQuotaTaskLogic(service)
	subscribes, err := logic.getSubscribes(ctx, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	if len(subscribes) != 1 || subscribes[0].Upload != 100 || subscribes[0].Token != "T1" {
		t.Fatalf("snapshot = %+v", subscribes)
	}

	// Mutations that land after the snapshot: traffic accounting adds 50
	// upload, an administrator rotates the credential.
	client.UserSubscribe.UpdateOneID(1).SetUpload(150).SetToken("T2").SetUUID("U2").ExecX(ctx)

	if err := logic.processSubscribes(ctx, subscribes, task.QuotaContent{Days: 10}, info); err != nil {
		t.Fatal(err)
	}

	stored := client.UserSubscribe.GetX(ctx, 1)
	if stored.Upload != 150 {
		t.Fatalf("quota grant reverted concurrently settled traffic: upload = %d, want 150", stored.Upload)
	}
	if stored.Download != 40 {
		t.Fatalf("quota grant reverted download: %d, want 40", stored.Download)
	}
	if stored.Token != "T2" || stored.UUID != "U2" {
		t.Fatalf("quota grant reverted the rotated credentials: token=%q uuid=%q, want T2/U2", stored.Token, stored.UUID)
	}
	// expire_time advanced by the grant: base + 24h (kept) + 10 days.
	want := base.Add(24*time.Hour).AddDate(0, 0, 10)
	if delta := stored.ExpireTime.Sub(want); delta > time.Minute || delta < -time.Minute {
		t.Fatalf("expire_time = %s, want about %s", stored.ExpireTime, want)
	}
}

// TestQuotaGrantExtendsFromLockedRowNotSnapshot proves the extension is computed
// from the locked row. The snapshot's expiry is stale by the time the grant
// commits, so extending the snapshot would undo a concurrent renewal.
func TestQuotaGrantExtendsFromLockedRowNotSnapshot(t *testing.T) {
	service, client := quotaFixture(t)
	ctx := context.Background()
	seedSubscription(t, client, 1, nil)
	info := seedTask(t, client, 1)

	logic := NewQuotaTaskLogic(service)
	subscribes, err := logic.getSubscribes(ctx, []int64{1})
	if err != nil {
		t.Fatal(err)
	}

	// A renewal pushes the expiry far out after the snapshot was taken.
	renewed := time.Now().Add(365 * 24 * time.Hour)
	client.UserSubscribe.UpdateOneID(1).SetExpireTime(renewed).ExecX(ctx)

	if err := logic.processSubscribes(ctx, subscribes, task.QuotaContent{Days: 5}, info); err != nil {
		t.Fatal(err)
	}

	stored := client.UserSubscribe.GetX(ctx, 1)
	want := renewed.AddDate(0, 0, 5)
	if delta := stored.ExpireTime.Sub(want); delta > time.Minute || delta < -time.Minute {
		t.Fatalf("expire_time = %s, want about %s (extended from the locked row, not the snapshot)", stored.ExpireTime, want)
	}
}

// TestQuotaResetTrafficZeroesCountersAndLogs covers the ResetTraffic branch:
// counters are zeroed on the locked row (including traffic that landed after the
// snapshot) and a reset-traffic system log is written.
func TestQuotaResetTrafficZeroesCountersAndLogs(t *testing.T) {
	service, client := quotaFixture(t)
	ctx := context.Background()
	seedSubscription(t, client, 1, nil)
	info := seedTask(t, client, 1)

	logic := NewQuotaTaskLogic(service)
	subscribes, err := logic.getSubscribes(ctx, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	// Traffic lands after the snapshot; a reset must clear it too.
	client.UserSubscribe.UpdateOneID(1).SetUpload(999).SetDownload(777).ExecX(ctx)

	if err := logic.processSubscribes(ctx, subscribes, task.QuotaContent{ResetTraffic: true}, info); err != nil {
		t.Fatal(err)
	}

	stored := client.UserSubscribe.GetX(ctx, 1)
	if stored.Upload != 0 || stored.Download != 0 {
		t.Fatalf("reset traffic left counters: upload=%d download=%d, want 0/0", stored.Upload, stored.Download)
	}
	if client.SystemLog.Query().Where(entsystemlog.TypeEQ(modellog.TypeResetSubscribe.Uint8())).CountX(ctx) == 0 {
		t.Fatal("traffic reset wrote no reset-traffic log")
	}
}

// TestQuotaGrantSkipsProviderManagedAndDeletedRows pins the two guards that stop
// the grant from touching a row it must not: a provider-managed subscription
// (entitlement_source != ""), which a local quota task must never edit, and a
// row deleted between the snapshot and the transaction, which must be skipped
// rather than failing the whole task.
func TestQuotaGrantSkipsProviderManagedAndDeletedRows(t *testing.T) {
	service, client := quotaFixture(t)
	ctx := context.Background()

	// Provider-managed row: untouched, and the task still completes.
	seedSubscription(t, client, 1, func(create *ent.UserSubscribeCreate) {
		create.SetEntitlementSource("apple")
	})
	info := seedTask(t, client, 1)
	logic := NewQuotaTaskLogic(service)

	managed, err := logic.getSubscribes(ctx, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	before := client.UserSubscribe.GetX(ctx, 1).ExpireTime
	if err := logic.processSubscribes(ctx, managed, task.QuotaContent{Days: 10}, info); err != nil {
		t.Fatal(err)
	}
	if after := client.UserSubscribe.GetX(ctx, 1).ExpireTime; !after.Equal(before) {
		t.Fatalf("provider-managed subscription was edited: %s -> %s", before, after)
	}
	if status := client.Task.GetX(ctx, 1).Status; status != 2 {
		t.Fatalf("task status = %d, want completed (2)", status)
	}

	// Deleted row: skipped as a soft failure, not a hard error.
	seedSubscription(t, client, 2, nil)
	info2 := seedTask(t, client, 2)
	stale, err := logic.getSubscribes(ctx, []int64{2})
	if err != nil {
		t.Fatal(err)
	}
	client.UserSubscribe.DeleteOneID(2).ExecX(ctx)
	if err := logic.processSubscribes(ctx, stale, task.QuotaContent{Days: 10}, info2); err != nil {
		t.Fatalf("deleted row failed the whole task: %v", err)
	}
	if _, err := client.UserSubscribe.Get(ctx, 2); !ent.IsNotFound(err) {
		t.Fatalf("deleted subscription came back: %v", err)
	}
	if stored := client.Task.GetX(ctx, 2); stored.Errors == "" {
		t.Fatal("deleted subscription was not reported as a soft failure")
	}
}

// TestQuotaGrantDoesNotResurrectTerminalRow checks the update predicate. A row
// that reached a terminal status (>= 4, e.g. cancelled or refunded) must not be
// extended or reactivated: the grant's CAS refuses it and the row is reported as
// a soft failure instead of being silently changed.
func TestQuotaGrantDoesNotResurrectTerminalRow(t *testing.T) {
	service, client := quotaFixture(t)
	ctx := context.Background()
	seedSubscription(t, client, 1, func(create *ent.UserSubscribeCreate) {
		create.SetStatus(4)
	})
	info := seedTask(t, client, 1)

	logic := NewQuotaTaskLogic(service)
	subscribes, err := logic.getSubscribes(ctx, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	before := client.UserSubscribe.GetX(ctx, 1)

	if err := logic.processSubscribes(ctx, subscribes, task.QuotaContent{Days: 10}, info); err != nil {
		t.Fatalf("terminal row failed the task instead of being reported: %v", err)
	}

	after := client.UserSubscribe.GetX(ctx, 1)
	if !after.ExpireTime.Equal(before.ExpireTime) || after.Status != 4 {
		t.Fatalf("terminal subscription was resurrected: expire %s -> %s, status %d -> %d",
			before.ExpireTime, after.ExpireTime, before.Status, after.Status)
	}
	if stored := client.Task.GetX(ctx, 1); stored.Errors == "" {
		t.Fatal("terminal row was silently granted without a reported failure")
	}
}

func TestRedeliveredQuotaGrantIsAppliedOnce(t *testing.T) {
	service, client := quotaFixture(t)
	ctx := t.Context()
	base := seedSubscription(t, client, 1, nil)
	info := seedTask(t, client, 1)
	duplicate := *info
	logic := NewQuotaTaskLogic(service)
	subs, err := logic.getSubscribes(ctx, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	for _, taskInfo := range []*task.Task{info, &duplicate} {
		if err := logic.processSubscribes(ctx, subs, task.QuotaContent{Days: 1}, taskInfo); err != nil {
			t.Fatal(err)
		}
	}
	if got := client.UserSubscribe.GetX(ctx, 1).ExpireTime; !got.Equal(base.Add(24*time.Hour).AddDate(0, 0, 1)) {
		t.Fatalf("replayed quota expiry=%v", got)
	}
}

func TestFailedGiftLogRollsBackGrantAndTaskClaim(t *testing.T) {
	service, db := quotaFixture(t)
	ctx := t.Context()
	seedSubscription(t, db, 1, nil)
	info := seedTask(t, db, 1)
	db.User.Create().SetID(7).SetPassword("fixture").SaveX(ctx)
	w := db.UserWallet.Create().SetUserID(7).SetBalance(50).SetGiftAmount(10).SaveX(ctx)
	db.SystemLog.Use(func(ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(context.Context, ent.Mutation) (ent.Value, error) {
			return nil, errors.New("gift log write failed")
		})
	})
	logic := NewQuotaTaskLogic(service)
	subs, err := logic.getSubscribes(ctx, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	err = logic.processSubscribes(ctx, subs, task.QuotaContent{Days: 1, GiftValue: 3, GiftType: 1}, info)
	if err == nil || db.UserWallet.GetX(ctx, w.ID).GiftAmount != 10 || db.Task.GetX(ctx, 1).Status != 0 || !db.UserSubscribe.GetX(ctx, 1).ExpireTime.Equal(subs[0].ExpireTime) {
		t.Fatalf("incomplete rollback after gift failure: %v", err)
	}
}
