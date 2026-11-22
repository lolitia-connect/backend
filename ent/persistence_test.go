package ent_test

import (
	"database/sql"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/ent/order"
	"github.com/perfect-panel/server/ent/orderevent"
	"github.com/perfect-panel/server/ent/userauthmethod"
	_ "modernc.org/sqlite"
)

func openSQLite(t *testing.T) *ent.Client {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
}

func TestOrderAndOutboxPersistAtomically(t *testing.T) {
	ctx := t.Context()
	client := openSQLite(t)
	placed := client.Order.Create().SetOrderNo("checkout-17").SetStateVersion(1).SaveX(ctx)
	const eventType = "order.paid"
	const payload = `{"state_version":2}`
	for _, commit := range []bool{false, true} {
		tx, err := client.Tx(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tx.Rollback() })
		if _, err := tx.Order.UpdateOneID(placed.ID).AddStateVersion(1).Save(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.OrderEvent.Create().SetOrderID(placed.ID).SetOrderNo(placed.OrderNo).SetEventType(eventType).SetPayload(payload).Save(ctx); err != nil {
			t.Fatal(err)
		}
		if commit {
			err = tx.Commit()
		} else {
			err = tx.Rollback()
		}
		if err != nil {
			t.Fatal(err)
		}
		current := client.Order.Query().Where(order.OrderNoEQ(placed.OrderNo)).OnlyX(ctx)
		outbox := client.OrderEvent.Query().Where(orderevent.OrderIDEQ(placed.ID), orderevent.PublishedAtIsNil()).AllX(ctx)
		if !commit {
			if current.StateVersion != 1 || len(outbox) != 0 {
				t.Fatalf("rollback left version %d and %d events", current.StateVersion, len(outbox))
			}
			continue
		}
		if current.StateVersion != 2 || len(outbox) != 1 || outbox[0].EventType != eventType || outbox[0].Payload != payload || outbox[0].OrderNo != placed.OrderNo {
			t.Fatalf("commit persisted version %d and events %v", current.StateVersion, outbox)
		}
		publishedAt := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
		client.OrderEvent.UpdateOneID(outbox[0].ID).SetPublishedAt(publishedAt).SaveX(ctx)
		if client.OrderEvent.Query().Where(orderevent.PublishedAtIsNil()).ExistX(ctx) {
			t.Fatal("published event still appears in pending outbox")
		}
		event := client.OrderEvent.GetX(ctx, outbox[0].ID)
		if event.PublishedAt == nil || !event.PublishedAt.Equal(publishedAt) {
			t.Fatalf("persisted publication time = %v, want %v", event.PublishedAt, publishedAt)
		}
	}
}

func TestAuthIdentifierIsUniquePerMethodAcrossAccounts(t *testing.T) {
	ctx := t.Context()
	client := openSQLite(t)
	const identifier = "shared@example.test"
	email := client.UserAuthMethod.Create().SetAuthType("email").SetAuthIdentifier(identifier).SetUserID(17).SaveX(ctx)
	_, err := client.UserAuthMethod.Create().SetAuthType("email").SetAuthIdentifier(identifier).SetUserID(18).Save(ctx)
	if !ent.IsConstraintError(err) {
		t.Fatalf("second account claimed email, error = %v", err)
	}
	provider := client.UserAuthMethod.Create().SetAuthType("oauth").SetAuthIdentifier(identifier).SetUserID(18).SaveX(ctx)
	if provider.ID == email.ID || provider.UserID != 18 {
		t.Fatalf("unexpected provider identity %v", provider)
	}
	client.UserAuthMethod.UpdateOneID(email.ID).SetVerified(true).SaveX(ctx)
	stored := client.UserAuthMethod.Query().Where(userauthmethod.AuthTypeEQ("email"), userauthmethod.AuthIdentifierEQ(identifier)).OnlyX(ctx)
	if !stored.Verified || stored.UserID != 17 {
		t.Fatalf("stored email identity = %v", stored)
	}
}
