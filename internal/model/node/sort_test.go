package node

import "testing"

func TestInsertServerAndNodeAssignSortOnEmptyTables(t *testing.T) {
	client := deletionClient(t)
	store := NewModel(client, nil)
	ctx := t.Context()
	for _, tc := range []struct{ requested, want int }{{0, 1}, {7, 7}, {0, 8}} {
		server := &Server{Name: "landing", Sort: tc.requested}
		if err := store.InsertServer(ctx, server); err != nil {
			t.Fatal(err)
		}
		n := &Node{Name: "entry", ServerId: server.Id, Sort: tc.requested}
		if err := store.InsertNode(ctx, n); err != nil {
			t.Fatal(err)
		}
		if got := client.Server.GetX(ctx, server.Id).Sort; got != tc.want {
			t.Fatalf("server sort = %d, want %d", got, tc.want)
		}
		if got := client.Node.GetX(ctx, n.Id).Sort; got != tc.want {
			t.Fatalf("node sort = %d, want %d", got, tc.want)
		}
	}
}
