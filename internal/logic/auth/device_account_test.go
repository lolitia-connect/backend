package auth

import (
	"database/sql"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/ent/systemlog"
	"github.com/perfect-panel/server/internal/middleware"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func TestDeviceAccountFollowsBindingAndDisablesDeviceOnlyOwner(t *testing.T) {
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
	service := &svc.ServiceContext{Store: repository.NewStore(client, cache), Redis: cache}
	service.Config.JwtAuth.AccessSecret = "device-fixture-secret"
	service.Config.JwtAuth.AccessExpire = 3600
	req := &types.DeviceLoginRequest{Identifier: "device-fixture", ShortCode: "A1B2", IP: "192.0.2.44", UserAgent: "Fixture client"}
	login := NewDeviceLoginLogic(ctx, service)
	if response, err := login.DeviceLogin(req); err == nil || response != nil || client.User.Query().CountX(ctx) != 0 {
		t.Fatal("a disabled device login provisioned an account")
	}
	service.Config.Device.Enable = true
	service.Config.Register.EnableTrial = true
	if response, err := login.DeviceLogin(req); err == nil || response != nil {
		t.Fatal("provisioning succeeded without the configured trial plan")
	}
	if client.User.Query().CountX(ctx) != 0 || client.UserDevice.Query().CountX(ctx) != 0 || client.UserAuthMethod.Query().CountX(ctx) != 0 {
		t.Fatal("failed trial provisioning left partial account records")
	}
	service.Config.Register.EnableTrial = false
	issue := func() string {
		t.Helper()
		response, err := login.DeviceLogin(req)
		if err != nil || response == nil || response.Token == "" {
			t.Fatalf("device login: %v", err)
		}
		return response.Token
	}
	assertOwner := func(token string, want int64) {
		t.Helper()
		authenticated, err := middleware.AuthenticateRequest(ctx, service, token, "/v1/public/user/info")
		if err != nil {
			t.Fatal(err)
		}
		account, ok := authenticated.Value(constant.CtxKeyUser).(*user.User)
		if !ok || account.Id != want {
			t.Fatalf("device authenticated as %+v, want user %d", account, want)
		}
	}
	originalToken := issue()
	original := client.User.Query().OnlyX(ctx)
	device := client.UserDevice.Query().OnlyX(ctx)
	credential := client.UserAuthMethod.Query().OnlyX(ctx)
	if device.UserID != original.ID || device.ShortCode != req.ShortCode || !device.Enabled ||
		credential.UserID != original.ID || credential.AuthIdentifier != req.Identifier || credential.AuthType != "device" || !credential.Verified {
		t.Fatal("provisioned device and authentication record do not match the account")
	}
	assertOwner(originalToken, original.ID)

	newOwner := client.User.Create().SetPassword("fixture").SaveX(ctx)
	client.UserAuthMethod.Create().SetUserID(newOwner.ID).SetAuthType("email").SetAuthIdentifier("device-owner@example.test").SaveX(ctx)
	binder := NewBindDeviceLogic(ctx, service)
	if err := binder.BindDeviceToUser(req.Identifier, "192.0.2.45", "Updated client", original.ID); err != nil {
		t.Fatal(err)
	}
	if got := client.UserDevice.GetX(ctx, device.ID); got.UserID != original.ID || got.IP != "192.0.2.45" || got.UserAgent == nil || *got.UserAgent != "Updated client" {
		t.Fatalf("same-owner binding did not refresh device metadata: %+v", got)
	}
	if err := binder.BindDeviceToUser(req.Identifier, "192.0.2.46", "Rebound client", newOwner.ID); err != nil {
		t.Fatal(err)
	}
	if client.User.GetX(ctx, original.ID).Enable || client.UserAuthMethod.GetX(ctx, credential.ID).UserID != newOwner.ID ||
		client.UserDevice.GetX(ctx, device.ID).UserID != newOwner.ID {
		t.Fatal("binding did not transfer the device and disable its device-only former owner")
	}
	if _, err := middleware.AuthenticateRequest(ctx, service, originalToken, "/v1/public/user/info"); err == nil {
		t.Fatal("disabled former owner can still use the original session")
	}
	assertOwner(issue(), newOwner.ID)
	client.UserDevice.UpdateOneID(device.ID).SetEnabled(false).SaveX(ctx)
	var code *xerr.CodeError
	if response, err := login.DeviceLogin(req); response != nil || !errors.As(err, &code) || code.GetErrCode() != xerr.UserDisabled {
		t.Fatalf("disabled device login = %+v, %v", response, err)
	}
	entries := client.SystemLog.Query().Where(systemlog.TypeEQ(log.TypeLogin.Uint8()), systemlog.ObjectIDEQ(newOwner.ID)).AllX(ctx)
	var succeeded, rejected bool
	for _, entry := range entries {
		var audit log.Login
		if err := audit.Unmarshal([]byte(entry.Content)); err != nil || audit.Method != "device" || audit.LoginIP != req.IP {
			t.Fatalf("invalid device login audit: %+v, %v", audit, err)
		}
		succeeded = succeeded || audit.Success
		rejected = rejected || !audit.Success
	}
	if len(entries) != 2 || !succeeded || !rejected {
		t.Fatal("device login audit did not distinguish issuance from rejection")
	}
}
