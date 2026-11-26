package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/model/auth"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/system"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestLoginSettingsAndAuditCommitTogether(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "settings.db")
	connect := func() *ent.Client {
		t.Helper()
		db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
		require.NoError(t, err)
		db.SetMaxOpenConns(1)
		client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
		t.Cleanup(func() { _ = client.Close() })
		return client
	}
	writer := connect()
	require.NoError(t, writer.Schema.Create(ctx))
	store, reader := NewStore(writer, nil), NewStore(connect(), nil)
	enabled := true
	method := &auth.Auth{Method: "email", Config: `{"verify":true}`, Enabled: &enabled}
	setting := &system.System{Category: "register", Key: "RegistrationEmail", Value: "required", Type: "string", Desc: "Email verification"}
	receipt := &log.SystemLog{Type: log.TypeAdminAction.Uint8(), Date: "2026-01-01", Content: `{"change":"enable email"}`}
	require.NoError(t, store.InTx(ctx, func(tx Store) error {
		if err := tx.Auth().Insert(ctx, method); err != nil {
			return err
		}
		if err := tx.System().Insert(ctx, setting); err != nil {
			return err
		}
		receipt.ObjectID = method.Id
		return tx.Log().Insert(ctx, receipt)
	}))
	failure := errors.New("audit correction unavailable")
	var offline bool
	writer.SystemLog.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if offline {
				return nil, failure
			}
			return next.Mutate(ctx, m)
		})
	})
	publish := func(tx Store) error {
		updated, err := tx.Auth().FindOne(ctx, method.Id)
		if err != nil {
			return err
		}
		*updated.Enabled = false
		if err = tx.Auth().Update(ctx, updated); err != nil {
			return err
		}
		policy, err := tx.System().FindOne(ctx, setting.Id)
		if err != nil {
			return err
		}
		policy.Value = "disabled"
		if err = tx.System().Update(ctx, policy); err != nil {
			return err
		}
		entry := *receipt
		entry.Content = `{"change":"disable email"}`
		return tx.Log().Update(ctx, &entry)
	}
	check := func(wantEnabled bool, wantValue, wantAudit string) {
		t.Helper()
		saved, err := reader.Auth().FindOneByMethod(ctx, method.Method)
		require.NoError(t, err)
		require.Equal(t, wantEnabled, *saved.Enabled)
		policy, err := reader.System().FindOneByKey(ctx, setting.Key)
		require.NoError(t, err)
		require.Equal(t, wantValue, policy.Value)
		entry, err := reader.Log().FindOne(ctx, receipt.Id)
		require.NoError(t, err)
		require.Equal(t, saved.Id, entry.ObjectID)
		require.JSONEq(t, wantAudit, entry.Content)
	}
	offline = true
	require.ErrorIs(t, store.InTx(ctx, publish), failure)
	check(true, "required", receipt.Content)
	offline = false
	require.NoError(t, store.InTx(ctx, publish))
	check(false, "disabled", `{"change":"disable email"}`)
}

func TestInTxRollsBackBeforePropagatingPanic(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	store := NewStore(client, nil)
	func() {
		defer func() {
			if r := recover(); r != "injected" {
				t.Fatalf("panic = %v", r)
			}
		}()
		_ = store.InTx(ctx, func(tx Store) error {
			tx.Ent().User.Create().SetPassword("fixture").SaveX(ctx)
			panic("injected")
		})
	}()
	count, err := client.User.Query().Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("connection after rollback: count=%d err=%v", count, err)
	}
}
