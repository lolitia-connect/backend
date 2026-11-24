package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	_ "modernc.org/sqlite"
)

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
