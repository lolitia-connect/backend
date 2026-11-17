package node

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	_ "modernc.org/sqlite"
)

var deleteFixtureID atomic.Uint64

func deletionClient(t *testing.T) *ent.Client {
	t.Helper()
	database, err := sql.Open("sqlite", fmt.Sprintf("file:server-delete-%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", deleteFixtureID.Add(1)))
	if err != nil {
		t.Fatal(err)
	}
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestServerDeletionRemovesOnlyAssociatedNodes(t *testing.T) {
	client := deletionClient(t)
	ctx := context.Background()
	client.Server.Create().SetID(1).SetSort(1).SaveX(ctx)
	client.Server.Create().SetID(2).SetSort(2).SaveX(ctx)
	client.Node.Create().SetID(1).SetServerID(1).SetSort(1).SaveX(ctx)
	client.Node.Create().SetID(2).SetServerID(2).SetSort(2).SaveX(ctx)
	client.Node.Create().SetID(3).SetServerID(1).SetSort(3).SaveX(ctx)
	client.ServerConfigOverride.Create().SetServerID(1).SaveX(ctx)
	client.ServerConfigOverride.Create().SetServerID(2).SaveX(ctx)
	model := NewModel(client, nil)
	for attempt := 0; attempt < 2; attempt++ {
		if err := model.DeleteServerWithOverrides(ctx, 1); err != nil {
			t.Fatal(err)
		}
	}
	if client.Server.Query().CountX(ctx) != 1 || client.Node.Query().CountX(ctx) != 1 || client.ServerConfigOverride.Query().CountX(ctx) != 1 {
		t.Fatal("server deletion left associated rows or removed unrelated rows")
	}
	if client.Node.GetX(ctx, 2).Sort != 1 || client.Server.GetX(ctx, 2).Sort != 1 {
		t.Fatal("server deletion did not preserve sort continuity")
	}
}

func TestServerDeletionRollsBackAssociatedRowsOnFailure(t *testing.T) {
	client := deletionClient(t)
	ctx := context.Background()
	client.Server.Create().SetID(1).SaveX(ctx)
	client.Node.Create().SetID(1).SetServerID(1).SaveX(ctx)
	if err := client.Driver().Exec(ctx, "DROP TABLE server_config_overrides", []any{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := NewModel(client, nil).DeleteServerWithOverrides(ctx, 1); err == nil {
		t.Fatal("override deletion failure ignored")
	}
	if client.Server.Query().CountX(ctx) != 1 || client.Node.Query().CountX(ctx) != 1 {
		t.Fatal("failed server deletion partially removed rows")
	}
}
