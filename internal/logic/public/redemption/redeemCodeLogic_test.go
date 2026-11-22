package redemption

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
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
