package listorder

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
	entnode "github.com/perfect-panel/server/ent/node"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/types"
	_ "modernc.org/sqlite"
)

var fixtureID atomic.Uint64

func testStore(t *testing.T) (*ent.Client, repository.Store) {
	t.Helper()
	database, err := sql.Open(
		"sqlite",
		fmt.Sprintf("file:listorder-%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", fixtureID.Add(1)),
	)
	if err != nil {
		t.Fatal(err)
	}
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	return client, repository.NewStore(client, nil)
}

func TestResetPlacesOneItemAtRequestedSortID(t *testing.T) {
	client, store := testStore(t)
	ctx := context.Background()
	for id := int64(1); id <= 3; id++ {
		client.Node.Create().SetID(id).SetName(fmt.Sprintf("node-%d", id)).SetSort(int(id - 1)).SaveX(ctx)
	}

	if err := Nodes.Reset(ctx, store, []types.SortItem{{Id: 3, Sort: 0}}); err != nil {
		t.Fatal(err)
	}
	rows, err := client.Node.Query().Order(entnode.BySort(), entnode.ByID()).All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for index, row := range rows {
		if row.Sort != index || row.ID != int64([]int64{3, 1, 2}[index]) {
			t.Fatalf("row %d = id %d, sort %d", index, row.ID, row.Sort)
		}
	}
	if err := Nodes.Reset(ctx, store, []types.SortItem{{Id: 1, Sort: 5}}); err != nil {
		t.Fatal(err)
	}
	rows, err = client.Node.Query().Order(entnode.BySort(), entnode.ByID()).All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rows[len(rows)-1].ID != 1 || rows[len(rows)-1].Sort != 5 {
		t.Fatalf("manual sort ID was not preserved: id %d, sort %d", rows[len(rows)-1].ID, rows[len(rows)-1].Sort)
	}
}

func TestByNameUsesTheSameNumericChineseOrder(t *testing.T) {
	client, store := testStore(t)
	ctx := context.Background()
	for id, name := range map[int64]string{1: "节点10", 2: "节点2", 3: "节点1"} {
		client.Node.Create().SetID(id).SetName(name).SetSort(int(id)).SaveX(ctx)
	}

	if err := Nodes.ByName(ctx, store); err != nil {
		t.Fatal(err)
	}
	rows, err := client.Node.Query().Order(entnode.BySort(), entnode.ByID()).All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for index, row := range rows {
		if row.ID != int64([]int64{3, 2, 1}[index]) || row.Sort != index {
			t.Fatalf("row %d = id %d, sort %d", index, row.ID, row.Sort)
		}
	}
}
