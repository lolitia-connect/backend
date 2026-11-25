package marketing

import (
	"database/sql"
	"slices"
	"strconv"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	modeltask "github.com/perfect-panel/server/internal/model/task"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	worker "github.com/perfect-panel/server/queue/logic/task"
	queuetypes "github.com/perfect-panel/server/queue/types"
	_ "modernc.org/sqlite"
)

func TestQuotaGrantQueuesAndAppliesOnlyToThePreviewedAudience(t *testing.T) {
	ctx := t.Context()
	database, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	db := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = db.Close() })
	options := asynq.RedisClientOpt{Addr: miniredis.RunT(t).Addr()}
	queue, inspector := asynq.NewClient(options), asynq.NewInspector(options)
	t.Cleanup(func() { _ = inspector.Close(); _ = queue.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(db, nil), Queue: queue}
	plan := db.Subscribe.Create().SetName("Standard").SaveX(ctx)
	other := db.Subscribe.Create().SetName("Other").SaveX(ctx)
	at := time.Now().Truncate(time.Second)
	expiry := at.AddDate(0, 0, 10)
	add := func(orderID, planID int64, status uint8, start time.Time) *ent.UserSubscribe {
		t.Helper()
		return db.UserSubscribe.Create().SetUserID(orderID).SetOrderID(orderID).SetSubscribeID(planID).
			SetStatus(status).SetStartTime(start).SetExpireTime(expiry).SetUpload(90).SetDownload(250).SaveX(ctx)
	}
	match := add(1, plan.ID, 1, at.AddDate(0, 0, -3))
	expired := add(2, plan.ID, 3, at.AddDate(0, 0, -3))
	wrongPlan := add(3, other.ID, 1, at.AddDate(0, 0, -3))
	tooNew := add(4, plan.ID, 1, at)
	active := true
	group := []string{strconv.FormatInt(plan.ID, 10)}
	start, end := at.AddDate(0, 0, -1).UnixMilli(), at.AddDate(0, 0, 1).UnixMilli()
	preCount, err := NewQueryQuotaTaskPreCountLogic(ctx, service).QueryQuotaTaskPreCount(&types.QueryQuotaTaskPreCountRequest{
		Subscribers: group, IsActive: &active, StartTime: start, EndTime: end,
	})
	if err != nil || preCount.Count != 1 {
		t.Fatalf("quota preview = %+v, %v", preCount, err)
	}
	create := NewCreateQuotaTaskLogic(ctx, service)
	if err := create.CreateQuotaTask(&types.CreateQuotaTaskRequest{Subscribers: group, StartTime: at.AddDate(0, 0, -60).UnixMilli(), ResetTraffic: true}); err == nil || db.Task.Query().CountX(ctx) != 0 {
		t.Fatal("empty preview created a quota task")
	}
	if err := create.CreateQuotaTask(&types.CreateQuotaTaskRequest{
		Subscribers: group, IsActive: &active, StartTime: start, EndTime: end, Days: 2, ResetTraffic: true,
	}); err != nil {
		t.Fatal(err)
	}
	stored := db.Task.Query().OnlyX(ctx)
	list, err := NewQueryQuotaTaskListLogic(ctx, service).QueryQuotaTaskList(&types.QueryQuotaTaskListRequest{})
	if err != nil || list.Total != 1 || len(list.List) != 1 {
		t.Fatalf("quota task list = %+v, %v", list, err)
	}
	if item := list.List[0]; item.Id != stored.ID || item.Total != 1 || item.Current != 0 || item.Days != 2 || !item.ResetTraffic ||
		!slices.Equal(item.Objects, []string{strconv.FormatInt(match.ID, 10)}) {
		t.Fatalf("quota task = %+v", item)
	}
	pending, err := inspector.ListPendingTasks("default")
	if err != nil || len(pending) != 1 || pending[0].Type != queuetypes.ForthwithQuotaTask || string(pending[0].Payload) != strconv.FormatInt(stored.ID, 10) {
		t.Fatalf("queued quota tasks = %d, %v", len(pending), err)
	}
	grant := worker.NewQuotaTaskLogic(service)
	for attempt := 0; attempt < 2; attempt++ {
		if err := grant.ProcessTask(ctx, asynq.NewTask(pending[0].Type, pending[0].Payload)); err != nil {
			t.Fatal(err)
		}
	}
	if applied := db.UserSubscribe.GetX(ctx, match.ID); applied.Upload != 0 || applied.Download != 0 || !applied.ExpireTime.Equal(expiry.AddDate(0, 0, 2)) {
		t.Fatalf("selected subscription quota after replay = %+v", applied)
	}
	for _, id := range []int64{expired.ID, wrongPlan.ID, tooNew.ID} {
		if unchanged := db.UserSubscribe.GetX(ctx, id); unchanged.Upload != 90 || unchanged.Download != 250 || !unchanged.ExpireTime.Equal(expiry) {
			t.Fatalf("excluded subscription %d was modified", id)
		}
		status, err := NewQueryQuotaTaskStatusLogic(ctx, service).QueryQuotaTaskStatus(&types.QueryQuotaTaskStatusRequest{Id: stored.ID})
		if err != nil || status.Status != uint8(modeltask.StatusCompleted) || status.Current != 1 || status.Total != 1 || status.Errors != "" {
			t.Fatalf("quota task status = %+v, %v", status, err)
		}
	}
}
