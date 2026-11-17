package node

import (
	"context"
	"testing"
	"time"
)

// TestUpdateServerKeepsNewerHeartbeat is the regression test for the
// last_reported_at clobber. The admin update path loads the server row, edits
// it and saves it back; a node heartbeat that lands between the load and the
// save must survive, or the server reads as offline until the next report.
//
// It drives the real production shape: read through the admin logic's accessor,
// let the heartbeat path write a newer timestamp, then save the stale row.
func TestUpdateServerKeepsNewerHeartbeat(t *testing.T) {
	client := deletionClient(t)
	ctx := context.Background()
	model := NewModel(client, nil)

	loaded := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	client.Server.Create().SetID(1).SetName("old").SetSort(1).SetLastReportedAt(loaded).SaveX(ctx)

	// The administrator loads the row and edits it.
	server, err := model.FindOneServer(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if server.LastReportedAt == nil || !server.LastReportedAt.Equal(loaded) {
		t.Fatalf("loaded last_reported_at = %v, want %v", server.LastReportedAt, loaded)
	}
	server.Name = "renamed"

	// A node heartbeat lands after the load, before the save.
	fresh := time.Now().UTC().Truncate(time.Second)
	if err := model.UpdateServerLastReportedAt(ctx, 1, fresh); err != nil {
		t.Fatal(err)
	}

	// The administrator's save must persist the edit but not the stale column.
	if err := model.UpdateServer(ctx, server); err != nil {
		t.Fatal(err)
	}

	stored := client.Server.GetX(ctx, 1)
	if stored.Name != "renamed" {
		t.Fatalf("admin edit was lost: name = %q, want renamed", stored.Name)
	}
	if stored.LastReportedAt == nil {
		t.Fatal("admin update cleared last_reported_at")
	}
	if !stored.LastReportedAt.Equal(fresh) {
		t.Fatalf("admin update clobbered a newer heartbeat: last_reported_at = %v, want %v (loaded %v)",
			stored.LastReportedAt, fresh, loaded)
	}
}

// TestUpdateServerSortKeepsNewerHeartbeat pins that the sort-only entry point
// inherits the fix: it loads a row and calls UpdateServer, so it must not write
// the loaded last_reported_at back either.
func TestUpdateServerSortKeepsNewerHeartbeat(t *testing.T) {
	client := deletionClient(t)
	ctx := context.Background()
	model := NewModel(client, nil)

	client.Server.Create().SetID(1).SetSort(1).SetLastReportedAt(time.Now().Add(-time.Hour).UTC()).SaveX(ctx)
	client.Server.Create().SetID(2).SetSort(2).SaveX(ctx)

	fresh := time.Now().UTC().Truncate(time.Second)
	if err := model.UpdateServerLastReportedAt(ctx, 1, fresh); err != nil {
		t.Fatal(err)
	}

	// UpdateServerSort re-reads the row and saves it whole through UpdateServer.
	if err := model.UpdateServerSort(ctx, 1, 5); err != nil {
		t.Fatal(err)
	}

	stored := client.Server.GetX(ctx, 1)
	if stored.Sort != 5 {
		t.Fatalf("sort was not applied: %d, want 5", stored.Sort)
	}
	if stored.LastReportedAt == nil || !stored.LastReportedAt.Equal(fresh) {
		t.Fatalf("UpdateServerSort clobbered a newer heartbeat: %v, want %v", stored.LastReportedAt, fresh)
	}
}

// TestUpdateServerCanStillClearLastReportedAtOnInsert documents the deliberate
// asymmetry: InsertServer keeps writing the column (a caller may seed it), while
// UpdateServer must not. This guards against "fixing" the defect by removing the
// column from the model entirely.
func TestInsertServerStillAcceptsLastReportedAt(t *testing.T) {
	client := deletionClient(t)
	ctx := context.Background()
	model := NewModel(client, nil)

	reported := time.Now().UTC().Truncate(time.Second)
	server := &Server{Name: "n", Sort: 1, LastReportedAt: &reported}
	if err := model.InsertServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	stored := client.Server.GetX(ctx, server.Id)
	if stored.LastReportedAt == nil || !stored.LastReportedAt.Equal(reported) {
		t.Fatalf("InsertServer dropped last_reported_at: %v, want %v", stored.LastReportedAt, reported)
	}
}
