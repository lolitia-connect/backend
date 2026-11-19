package auth

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func TestTelephoneRegisterCreatesAccountAndSession(t *testing.T) {
	database, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	mini := miniredis.RunT(t)
	cache := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, cache), Redis: cache}
	service.Config.Mobile.Enable = true
	service.Config.JwtAuth.AccessSecret = "telephone-registration-test-secret"
	service.Config.JwtAuth.AccessExpire = 3600
	ctx := context.Background()
	key := fmt.Sprintf("%s:%s:%s", config.AuthCodeTelephoneCacheKey, constant.ParseVerifyType(uint8(constant.Register)), "+12025550123")
	if err := cache.Set(ctx, key, `{"code":"123456"}`, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	request := &types.TelephoneRegisterRequest{TelephoneAreaCode: "1", Telephone: "2025550123", Password: "test-only-password", Code: "123456", IP: "192.0.2.1", UserAgent: "test-agent"}
	response, err := NewTelephoneRegisterLogic(ctx, service).TelephoneRegister(request)
	if err != nil || response == nil || response.Token == "" {
		t.Fatalf("registration did not return a session: response=%+v, err=%v", response, err)
	}
	if client.User.Query().CountX(ctx) != 1 || client.UserAuthMethod.Query().OnlyX(ctx).AuthIdentifier != "+12025550123" || cache.Exists(ctx, key).Val() != 0 {
		t.Fatal("telephone account was not persisted or verification code was not consumed")
	}
}

func TestTelephoneRegisterHonorsDisabledRegistration(t *testing.T) {
	service := &svc.ServiceContext{}
	service.Config.Register.StopRegister = true
	if response, err := NewTelephoneRegisterLogic(context.Background(), service).TelephoneRegister(&types.TelephoneRegisterRequest{}); err == nil || response != nil {
		t.Fatalf("disabled registration returned success: %+v, %v", response, err)
	}
}
