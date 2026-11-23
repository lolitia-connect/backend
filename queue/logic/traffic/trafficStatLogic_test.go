package traffic

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-sql-driver/mysql"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/ent/systemlog"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tasklock"
	"github.com/perfect-panel/server/queue/types"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func trafficStatTestService(t *testing.T) (*svc.ServiceContext, *asynq.Inspector) {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	addr := miniredis.RunT(t).Addr()
	cache := redis.NewClient(&redis.Options{Addr: addr})
	opt := asynq.RedisClientOpt{Addr: addr, DB: 5}
	queue, inspector := asynq.NewClient(opt), asynq.NewInspector(opt)
	service := &svc.ServiceContext{Ent: client, Store: repository.NewStore(client, cache), Redis: cache, Queue: queue}
	service.Config.Log.AutoClear = true
	service.Config.Log.ClearDays = 7
	t.Cleanup(func() { _ = service.Queue.Close(); _ = inspector.Close(); _ = cache.Close(); _ = client.Close() })
	return service, inspector
}

func seedDailyTraffic(t *testing.T, service *svc.ServiceContext) time.Time {
	t.Helper()
	now := time.Now()
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	service.Ent.TrafficLog.Create().SetServerID(1).SetUserID(2).SetSubscribeID(3).
		SetUpload(10).SetDownload(20).SetTimestamp(end.Add(-time.Hour)).SaveX(t.Context())
	return end
}

func pendingCleanup(t *testing.T, inspector *asynq.Inspector) *asynq.Task {
	t.Helper()
	pending, err := inspector.ListPendingTasks("default")
	if err != nil || len(pending) != 1 || pending[0].Type != types.CleanupTrafficLogs {
		t.Fatalf("pending cleanup = %+v, %v", pending, err)
	}
	return asynq.NewTask(pending[0].Type, pending[0].Payload)
}

func TestTrafficStatCommitsBeforeCleanupAndRemainsIdempotent(t *testing.T) {
	service, inspector := trafficStatTestService(t)
	end := seedDailyTraffic(t, service)
	ctx := t.Context()
	conflict := &mysql.MySQLError{Number: 1020}
	deletes := 0
	service.Ent.TrafficLog.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if mutation.Op().Is(ent.OpDelete) {
				deletes++
				return nil, conflict
			}
			return next.Mutate(ctx, mutation)
		})
	})
	stat := NewStatLogic(service)
	if err := stat.ProcessTask(ctx, asynq.NewTask(types.SchedulerTrafficStat, nil)); err != nil {
		t.Fatal(err)
	}
	if deletes != 0 || service.Ent.SystemLog.Query().CountX(ctx) != 5 {
		t.Fatal("statistics must commit without attempting cleanup")
	}
	queued := pendingCleanup(t, inspector)
	var payload types.TrafficLogCleanup
	if err := json.Unmarshal(queued.Payload(), &payload); err != nil || !payload.Cutoff.Equal(end.AddDate(0, 0, -7)) {
		t.Fatalf("cleanup payload = %+v, %v", payload, err)
	}
	service.Ent.TrafficLog.Create().SetServerID(1).SetUserID(2).SetSubscribeID(3).
		SetTimestamp(payload.Cutoff.Add(-time.Hour)).SaveX(ctx)
	if err := NewCleanupTrafficLogsLogic(service).ProcessTask(ctx, queued); !errors.Is(err, conflict) || deletes != 1 {
		t.Fatalf("cleanup must return the first conflict to Asynq: calls=%d err=%v", deletes, err)
	}
	if err := stat.ProcessTask(ctx, asynq.NewTask(types.SchedulerTrafficStat, nil)); err != nil {
		t.Fatal(err)
	}
	if service.Ent.SystemLog.Query().CountX(ctx) != 5 {
		t.Fatal("repeated statistics duplicated committed logs")
	}
	pendingCleanup(t, inspector)
	if service.Ent.TrafficLog.Query().CountX(ctx) != 2 {
		t.Fatal("failed cleanup must preserve traffic logs")
	}
}

func TestTrafficStatRollsBackAndReturnsFirstError(t *testing.T) {
	service, inspector := trafficStatTestService(t)
	seedDailyTraffic(t, service)
	ctx := t.Context()
	conflict := &mysql.MySQLError{Number: 1020}
	attempts := 0
	fail := true
	service.Ent.SystemLog.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if value, ok := mutation.(*ent.SystemLogMutation); ok {
				if typ, _ := value.GetType(); typ == log.TypeTrafficStat.Uint8() {
					attempts++
					if fail {
						return nil, conflict
					}
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
	logic := NewStatLogic(service)
	if err := logic.ProcessTask(ctx, asynq.NewTask(types.SchedulerTrafficStat, nil)); !errors.Is(err, conflict) || attempts != 1 {
		t.Fatalf("statistics must not retry internally: attempts=%d err=%v", attempts, err)
	}
	if service.Ent.SystemLog.Query().CountX(ctx) != 0 {
		t.Fatal("failed statistics must roll back all ranking entries")
	}
	queues, err := inspector.Queues()
	if err != nil || len(queues) != 0 {
		t.Fatalf("failed statistics enqueued cleanup: %v, %v", queues, err)
	}
	fail = false
	if err := logic.ProcessTask(ctx, asynq.NewTask(types.SchedulerTrafficStat, nil)); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || service.Ent.SystemLog.Query().Where(systemlog.Type(log.TypeTrafficStat.Uint8())).CountX(ctx) != 1 {
		t.Fatal("outer retry must commit exactly one summary")
	}
	pendingCleanup(t, inspector)
}

func TestTrafficStatRecoversCleanupEnqueueFailure(t *testing.T) {
	service, inspector := trafficStatTestService(t)
	seedDailyTraffic(t, service)
	ctx := t.Context()
	_ = service.Queue.Close()
	logic := NewStatLogic(service)
	if err := logic.ProcessTask(ctx, asynq.NewTask(types.SchedulerTrafficStat, nil)); err == nil {
		t.Fatal("enqueue failure must reach Asynq")
	}
	if service.Ent.SystemLog.Query().CountX(ctx) != 5 {
		t.Fatal("enqueue failure rolled back committed statistics")
	}
	service.Queue = asynq.NewClient(asynq.RedisClientOpt{Addr: service.Redis.Options().Addr, DB: 5})
	if err := logic.ProcessTask(ctx, asynq.NewTask(types.SchedulerTrafficStat, nil)); err != nil {
		t.Fatal(err)
	}
	if service.Ent.SystemLog.Query().CountX(ctx) != 5 {
		t.Fatal("enqueue retry duplicated statistics")
	}
	pendingCleanup(t, inspector)
}

func TestTrafficCleanupBatchesPersistAcrossOuterRetry(t *testing.T) {
	service, _ := trafficStatTestService(t)
	ctx := t.Context()
	cutoff := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	rows := make([]*ent.TrafficLogCreate, 0, 1002)
	for i := 0; i < 1001; i++ {
		rows = append(rows, service.Ent.TrafficLog.Create().SetServerID(1).SetUserID(2).SetSubscribeID(3).SetTimestamp(cutoff.Add(-time.Hour)))
	}
	rows = append(rows, service.Ent.TrafficLog.Create().SetServerID(1).SetUserID(2).SetSubscribeID(3).SetTimestamp(cutoff))
	service.Ent.TrafficLog.CreateBulk(rows...).SaveX(ctx)
	recent := service.Ent.TrafficLog.Create().SetServerID(1).SetUserID(2).SetSubscribeID(3).SetTimestamp(cutoff.Add(time.Nanosecond)).SaveX(ctx)
	conflict := &mysql.MySQLError{Number: 1020}
	calls := 0
	service.Ent.TrafficLog.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if mutation.Op().Is(ent.OpDelete) {
				calls++
				if calls == 2 {
					return nil, conflict
				}
				value, err := next.Mutate(ctx, mutation)
				if err == nil && value.(int) > 1000 {
					t.Errorf("deleted more than 1000 rows in one batch: %v", value)
				}
				return value, err
			}
			return next.Mutate(ctx, mutation)
		})
	})
	payload, _ := json.Marshal(types.TrafficLogCleanup{Cutoff: cutoff})
	queued := asynq.NewTask(types.CleanupTrafficLogs, payload)
	logic := NewCleanupTrafficLogsLogic(service)
	if err := logic.ProcessTask(ctx, queued); !errors.Is(err, conflict) || calls != 2 {
		t.Fatalf("cleanup must stop at first failed batch: calls=%d err=%v", calls, err)
	}
	if got := service.Ent.TrafficLog.Query().CountX(ctx); got != 3 {
		t.Fatalf("completed batch must remain committed: remaining=%d", got)
	}
	if err := logic.ProcessTask(ctx, queued); err != nil {
		t.Fatal(err)
	}
	if got := service.Ent.TrafficLog.Query().OnlyX(ctx); got.ID != recent.ID {
		t.Fatalf("cleanup must preserve logs newer than the cutoff: %+v", got)
	}
	if err := logic.ProcessTask(ctx, queued); err != nil || calls != 3 {
		t.Fatalf("completed cleanup must be safe to repeat: calls=%d err=%v", calls, err)
	}
}

func TestTrafficTaskLocksPreventConcurrentWorkers(t *testing.T) {
	service, _ := trafficStatTestService(t)
	end := seedDailyTraffic(t, service)
	ctx := t.Context()
	for _, key := range []string{"task:traffic:stat:" + end.AddDate(0, 0, -1).Format(time.DateOnly), "task:traffic:cleanup"} {
		lock, held, err := tasklock.Acquire(ctx, service.Redis, key, time.Minute)
		if err != nil || !held {
			t.Fatalf("acquire lock: %v", err)
		}
		if key == "task:traffic:cleanup" {
			payload, _ := json.Marshal(types.TrafficLogCleanup{Cutoff: end})
			err = NewCleanupTrafficLogsLogic(service).ProcessTask(ctx, asynq.NewTask(types.CleanupTrafficLogs, payload))
		} else {
			err = NewStatLogic(service).ProcessTask(ctx, asynq.NewTask(types.SchedulerTrafficStat, nil))
		}
		if err == nil {
			t.Fatalf("worker ran while lock was held: %s", key)
		}
		_, _ = lock.Release(ctx)
	}
	if service.Ent.SystemLog.Query().CountX(ctx) != 0 || service.Ent.TrafficLog.Query().CountX(ctx) != 1 {
		t.Fatal("concurrent worker changed statistics or traffic logs")
	}
}

func TestTrafficCleanupDisabledAndInvalidPayload(t *testing.T) {
	service, inspector := trafficStatTestService(t)
	seedDailyTraffic(t, service)
	ctx := t.Context()
	service.Config.Log.AutoClear = false
	if err := NewStatLogic(service).ProcessTask(ctx, asynq.NewTask(types.SchedulerTrafficStat, nil)); err != nil {
		t.Fatal(err)
	}
	if queues, err := inspector.Queues(); err != nil || len(queues) != 0 {
		t.Fatalf("disabled cleanup enqueued a task: %v, %v", queues, err)
	}
	logic := NewCleanupTrafficLogsLogic(service)
	if err := logic.ProcessTask(ctx, asynq.NewTask(types.CleanupTrafficLogs, nil)); err != nil {
		t.Fatal(err)
	}
	service.Config.Log.AutoClear = true
	for _, payload := range []string{"", "{", "{}"} {
		if err := logic.ProcessTask(ctx, asynq.NewTask(types.CleanupTrafficLogs, []byte(payload))); !errors.Is(err, asynq.SkipRetry) {
			t.Fatalf("invalid cleanup payload should not retry: %q, %v", payload, err)
		}
	}
	if service.Ent.TrafficLog.Query().CountX(ctx) != 1 {
		t.Fatal("disabled or invalid cleanup deleted traffic logs")
	}
}
