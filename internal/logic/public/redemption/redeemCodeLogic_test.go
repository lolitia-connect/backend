package redemption

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	_ "modernc.org/sqlite"
)

func newRedeemFixture(t *testing.T) (context.Context, *svc.ServiceContext, *ent.Client) {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	queue := asynq.NewClient(asynq.RedisClientOpt{Addr: cache.Options().Addr})
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil), Redis: cache, Queue: queue}
	t.Cleanup(func() {
		_ = queue.Close()
		_ = cache.Close()
		_ = client.Close()
		_ = db.Close()
	})
	return context.WithValue(t.Context(), constant.CtxKeyUser, &user.User{Id: 7}), service, client
}

func TestRedeemCodeRejectsInvalidStatesAndCleansUpEnqueueFailures(t *testing.T) {
	tests := []struct {
		name  string
		setup func(context.Context, *svc.ServiceContext, *ent.Client) string
		code  uint32
	}{
		{
			name:  "missing code",
			setup: func(ctx context.Context, _ *svc.ServiceContext, _ *ent.Client) string { return "missing" },
			code:  xerr.InvalidParams,
		},
		{
			name: "disabled code",
			setup: func(ctx context.Context, _ *svc.ServiceContext, db *ent.Client) string {
				plan := db.Subscribe.Create().SetSell(true).SaveX(ctx)
				return db.RedemptionCode.Create().SetCode("disabled").SetSubscribePlan(plan.ID).SetStatus(0).SaveX(ctx).Code
			},
			code: xerr.InvalidParams,
		},
		{
			name: "exhausted code",
			setup: func(ctx context.Context, _ *svc.ServiceContext, db *ent.Client) string {
				plan := db.Subscribe.Create().SetSell(true).SaveX(ctx)
				return db.RedemptionCode.Create().SetCode("exhausted").SetSubscribePlan(plan.ID).SetTotalCount(1).SetUsedCount(1).SaveX(ctx).Code
			},
			code: xerr.InvalidParams,
		},
		{
			name: "already redeemed",
			setup: func(ctx context.Context, _ *svc.ServiceContext, db *ent.Client) string {
				plan := db.Subscribe.Create().SetSell(true).SaveX(ctx)
				code := db.RedemptionCode.Create().SetCode("duplicate").SetSubscribePlan(plan.ID).SaveX(ctx)
				db.RedemptionRecord.Create().SetRedemptionCodeID(code.ID).SetUserID(7).SetSubscribeID(plan.ID).SaveX(ctx)
				return code.Code
			},
			code: xerr.InvalidParams,
		},
		{
			name: "missing plan",
			setup: func(ctx context.Context, _ *svc.ServiceContext, db *ent.Client) string {
				return db.RedemptionCode.Create().SetCode("missing-plan").SetSubscribePlan(999).SaveX(ctx).Code
			},
			code: xerr.SubscribeNotAvailable,
		},
		{
			name: "quota exceeded",
			setup: func(ctx context.Context, _ *svc.ServiceContext, db *ent.Client) string {
				plan := db.Subscribe.Create().SetSell(true).SetQuota(1).SaveX(ctx)
				db.UserSubscribe.Create().SetUserID(7).SetOrderID(1).SetSubscribeID(plan.ID).SaveX(ctx)
				return db.RedemptionCode.Create().SetCode("quota").SetSubscribePlan(plan.ID).SaveX(ctx).Code
			},
			code: xerr.SubscribeQuotaLimit,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, service, db := newRedeemFixture(t)
			code := tc.setup(ctx, service, db)
			_, err := NewRedeemCodeLogic(ctx, service).RedeemCode(&types.RedeemCodeRequest{Code: code})
			var appErr *xerr.CodeError
			if !errors.As(err, &appErr) || appErr.GetErrCode() != tc.code {
				t.Fatalf("redemption error = %v, want code %d", err, tc.code)
			}
			if got := db.Order.Query().CountX(ctx); got != 0 {
				t.Fatalf("rejected redemption created %d orders", got)
			}
		})
	}

	t.Run("lock already held", func(t *testing.T) {
		ctx, service, db := newRedeemFixture(t)
		plan := db.Subscribe.Create().SetSell(true).SaveX(ctx)
		code := db.RedemptionCode.Create().SetCode("locked").SetSubscribePlan(plan.ID).SaveX(ctx)
		if err := service.Redis.Set(ctx, "redemption_lock:7:locked", "1", time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
		_, err := NewRedeemCodeLogic(ctx, service).RedeemCode(&types.RedeemCodeRequest{Code: code.Code})
		var appErr *xerr.CodeError
		if !errors.As(err, &appErr) || appErr.GetErrCode() != xerr.TooManyRequests {
			t.Fatalf("locked redemption error = %v", err)
		}
	})

	t.Run("enqueue failure removes staged order", func(t *testing.T) {
		ctx, service, db := newRedeemFixture(t)
		plan := db.Subscribe.Create().SetSell(true).SaveX(ctx)
		code := db.RedemptionCode.Create().SetCode("queue-failure").SetSubscribePlan(plan.ID).SetUnitTime("day").SetQuantity(2).SaveX(ctx)
		if err := service.Queue.Close(); err != nil {
			t.Fatal(err)
		}
		_, err := NewRedeemCodeLogic(ctx, service).RedeemCode(&types.RedeemCodeRequest{Code: code.Code})
		var appErr *xerr.CodeError
		if !errors.As(err, &appErr) || appErr.GetErrCode() != xerr.ERROR {
			t.Fatalf("enqueue failure = %v", err)
		}
		if got := db.Order.Query().CountX(ctx); got != 0 {
			t.Fatalf("staged order survived enqueue failure: %d", got)
		}
	})
}

func TestUnavailablePlanAndFailedPlanLookup(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &user.User{Id: 7})
	plan := client.Subscribe.Create().SetSell(false).SaveX(ctx)
	code := client.RedemptionCode.Create().SetCode("test-redemption").SetSubscribePlan(plan.ID).SaveX(ctx)
	core, logs := observer.New(zap.ErrorLevel)
	logic := NewRedeemCodeLogic(ctx, &svc.ServiceContext{Store: repository.NewStore(client, nil), Redis: cache})
	logic.Logger = zap.New(core).Sugar()
	request := &types.RedeemCodeRequest{Code: code.Code}
	_, err = logic.RedeemCode(request)
	var appErr *xerr.CodeError
	if !errors.As(err, &appErr) || appErr.GetErrCode() != xerr.SubscribeNotAvailable || logs.Len() != 0 {
		t.Fatalf("unavailable plan: err=%v error logs=%d", err, logs.Len())
	}
	if _, err := db.Exec("DROP TABLE subscribe"); err != nil {
		t.Fatal(err)
	}
	_, err = logic.RedeemCode(request)
	if !errors.As(err, &appErr) || appErr.GetErrCode() != xerr.DatabaseQueryError || logs.Len() != 1 {
		t.Fatalf("failed lookup: err=%v error logs=%d", err, logs.Len())
	}
}

func TestFirstRedemptionDoesNotCreateNewPurchase(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	queue := asynq.NewClient(asynq.RedisClientOpt{Addr: cache.Options().Addr})
	t.Cleanup(func() { _ = queue.Close() })
	ctx := context.WithValue(t.Context(), constant.CtxKeyUser, &user.User{Id: 7})
	plan := client.Subscribe.Create().SetSell(true).SaveX(ctx)
	code := client.RedemptionCode.Create().SetCode("first-redemption").SetSubscribePlan(plan.ID).SaveX(ctx)
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil), Redis: cache, Queue: queue}
	if _, err := NewRedeemCodeLogic(ctx, service).RedeemCode(&types.RedeemCodeRequest{Code: code.Code}); err != nil {
		t.Fatal(err)
	}
	created := client.Order.Query().OnlyX(ctx)
	if created.Type != order.TypeRedemption || created.IsNew || created.Amount != 0 {
		t.Fatalf("redemption order = %+v", created)
	}
	if eligible, err := service.Store.Order().IsUserEligibleForNewOrder(ctx, 7); err != nil || !eligible {
		t.Fatalf("first paid purchase eligibility = %v, %v", eligible, err)
	}
}
