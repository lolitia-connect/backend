package auth

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

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

func newDeviceAccountService(t *testing.T) (*svc.ServiceContext, *ent.Client) {
	t.Helper()
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
	return service, client
}

func TestFirstDeviceBindingCanRetryAfterEitherWriteFails(t *testing.T) {
	service, client := newDeviceAccountService(t)
	ctx := t.Context()
	owner := client.User.Create().SetPassword("fixture").SetEnable(true).SaveX(ctx)
	email := client.UserAuthMethod.Create().SetUserID(owner.ID).SetAuthType("email").SetAuthIdentifier("reader@example.test").SetVerified(true).SaveX(ctx)
	req := &types.DeviceLoginRequest{Identifier: "reading-tablet", IP: "192.0.2.17", UserAgent: "Tablet"}
	binder := NewBindDeviceLogic(ctx, service)
	var blocked string
	client.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if m.Op().Is(ent.OpCreate) && m.Type() == blocked {
				return nil, errors.New("registration storage read-only")
			}
			return next.Mutate(ctx, m)
		})
	})
	for _, blocked = range []string{ent.TypeUserAuthMethod, ent.TypeUserDevice} {
		err := binder.BindDeviceToUser(req.Identifier, req.IP, req.UserAgent, owner.ID)
		var writeErr *xerr.CodeError
		if !errors.As(err, &writeErr) || writeErr.GetErrCode() != xerr.DatabaseInsertError {
			t.Fatalf("first binding with unavailable %s: %v", blocked, err)
		}
		if client.UserDevice.Query().CountX(ctx) != 0 || client.UserAuthMethod.Query().OnlyX(ctx).ID != email.ID {
			t.Fatal("failed device registration changed the current login methods")
		}
	}
	blocked = ""
	for range 2 {
		if err := binder.BindDeviceToUser(req.Identifier, req.IP, req.UserAgent, owner.ID); err != nil {
			t.Fatal(err)
		}
	}
	bound := client.UserDevice.Query().OnlyX(ctx)
	if bound.UserID != owner.ID || bound.Identifier != req.Identifier || bound.IP != req.IP || !bound.Enabled || client.UserAuthMethod.Query().CountX(ctx) != 2 {
		t.Fatal("retried binding did not preserve both the email and device logins")
	}
	service.Config.Device.Enable = true
	issued, err := NewDeviceLoginLogic(ctx, service).DeviceLogin(req)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := middleware.AuthenticateRequest(ctx, service, issued.Token, "/v1/public/user/info")
	if err != nil || identity.Value(constant.CtxKeyUser).(*user.User).Id != owner.ID || client.User.Query().CountX(ctx) != 1 {
		t.Fatalf("device login after recovery did not return the original account: %v", err)
	}
}

func TestDeviceTrialRegistrationRetriesAsOneGrant(t *testing.T) {
	service, client := newDeviceAccountService(t)
	ctx := t.Context()
	plan := client.Subscribe.Create().SetName("Device trial").SetTraffic(4096).SaveX(ctx)
	service.Config.Device.Enable = true
	service.Config.Register.EnableTrial = true
	service.Config.Register.TrialSubscribe = plan.ID
	service.Config.Register.TrialTime, service.Config.Register.TrialTimeUnit = 45, "Minute"
	login := NewDeviceLoginLogic(ctx, service)
	req := &types.DeviceLoginRequest{Identifier: "trial-tablet", ShortCode: "B123", IP: "192.0.2.42", UserAgent: "Tablet"}
	type write struct {
		entity string
		op     ent.Op
	}
	var blocked write
	client.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if m.Type() == blocked.entity && m.Op().Is(blocked.op) {
				return nil, errors.New("trial registration interrupted")
			}
			return next.Mutate(ctx, m)
		})
	})
	for _, blocked = range []write{{ent.TypeUser, ent.OpCreate}, {ent.TypeUser, ent.OpUpdateOne}, {ent.TypeUserAuthMethod, ent.OpCreate}, {ent.TypeUserDevice, ent.OpCreate}, {ent.TypeUserSubscribe, ent.OpCreate}} {
		if result, err := login.DeviceLogin(req); err == nil || result != nil {
			t.Fatalf("interrupted %s %s returned a session: %v", blocked.entity, blocked.op, err)
		}
		if client.User.Query().CountX(ctx) != 0 || client.UserAuthMethod.Query().CountX(ctx) != 0 || client.UserDevice.Query().CountX(ctx) != 0 || client.UserSubscribe.Query().CountX(ctx) != 0 {
			t.Fatalf("interrupted %s %s left a partial enrollment", blocked.entity, blocked.op)
		}
	}
	blocked = write{}
	checkSession := func() int64 {
		t.Helper()
		result, err := login.DeviceLogin(req)
		if err != nil {
			t.Fatal(err)
		}
		identity, err := middleware.AuthenticateRequest(ctx, service, result.Token, "/v1/public/user/info")
		if err != nil {
			t.Fatal(err)
		}
		return identity.Value(constant.CtxKeyUser).(*user.User).Id
	}
	owner := checkSession()
	grant := client.UserSubscribe.Query().OnlyX(ctx)
	if grant.UserID != owner || grant.SubscribeID != plan.ID || grant.Traffic != 4096 || grant.ExpireTime.Sub(grant.StartTime) != 45*time.Minute {
		t.Fatalf("unexpected trial for account %d: %+v", owner, grant)
	}
	client.UserSubscribe.UpdateOneID(grant.ID).SetDownload(512).ExecX(ctx)
	if got := checkSession(); got != owner {
		t.Fatalf("second login changed owner to %d", got)
	}
	current := client.UserSubscribe.Query().OnlyX(ctx)
	if current.ID != grant.ID || !current.ExpireTime.Equal(grant.ExpireTime) || current.Download != 512 {
		t.Fatalf("returning device was granted a fresh trial: %+v", current)
	}
}

func TestDeviceAccountFollowsBindingAndDisablesDeviceOnlyOwner(t *testing.T) {
	ctx := t.Context()
	service, client := newDeviceAccountService(t)
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
	var unavailable string
	client.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if m.Type() == unavailable {
				return nil, errors.New("device transfer write unavailable")
			}
			return next.Mutate(ctx, m)
		})
	})
	for _, unavailable = range []string{ent.TypeUser, ent.TypeUserAuthMethod, ent.TypeUserDevice} {
		err := binder.BindDeviceToUser(req.Identifier, "192.0.2.46", "Rebound client", newOwner.ID)
		var writeErr *xerr.CodeError
		if !errors.As(err, &writeErr) || writeErr.GetErrCode() != xerr.DatabaseUpdateError {
			t.Fatalf("binding with unavailable %s returned %v", unavailable, err)
		}
		stored := client.UserDevice.GetX(ctx, device.ID)
		if !client.User.GetX(ctx, original.ID).Enable || client.UserAuthMethod.GetX(ctx, credential.ID).UserID != original.ID ||
			stored.UserID != original.ID || stored.IP != "192.0.2.45" || stored.UserAgent == nil || *stored.UserAgent != "Updated client" {
			t.Fatalf("failed %s write partially transferred the device", unavailable)
		}
		assertOwner(originalToken, original.ID)
	}
	unavailable = ""
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
