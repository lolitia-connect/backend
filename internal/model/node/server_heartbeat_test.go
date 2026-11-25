package node

import (
	"context"
	"testing"
	"time"

	"github.com/perfect-panel/server/ent"
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

func TestServerHeartbeatBatchKeepsLatestReportAcrossBatches(t *testing.T) {
	ctx := t.Context()
	client := deletionClient(t)
	model := NewModel(client, nil)
	cutoff := time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC)
	older, newer := cutoff.Add(-time.Minute), cutoff.Add(time.Minute)

	creates := make([]*ent.ServerCreate, 0, 202)
	reports := make(map[int64]time.Time)
	for id := int64(1); id <= 201; id++ {
		creates = append(creates, client.Server.Create().SetID(id))
		reports[id] = newer
	}
	creates[0].SetLastReportedAt(newer)
	reports[1] = older
	creates[1].SetLastReportedAt(cutoff)
	reports[2] = cutoff
	creates = append(creates, client.Server.Create().SetID(202))
	if _, err := client.Server.CreateBulk(creates...).Save(ctx); err != nil {
		t.Fatal(err)
	}

	if err := model.UpdateServerLastReportedAtBatch(ctx, reports); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		id   int64
		want time.Time
	}{{1, newer}, {2, cutoff}, {3, newer}, {200, newer}, {201, newer}} {
		stored := client.Server.GetX(ctx, check.id)
		if stored.LastReportedAt == nil || !stored.LastReportedAt.Equal(check.want) {
			t.Fatalf("server %d last report = %v; want %v", check.id, stored.LastReportedAt, check.want)
		}
	}
	if stored := client.Server.GetX(ctx, 202); stored.LastReportedAt != nil {
		t.Fatalf("server without a report acquired timestamp %v", stored.LastReportedAt)
	}
	online, offline, err := model.CountServersByReportStatus(ctx, cutoff)
	if err != nil || online != 200 || offline != 2 {
		t.Fatalf("server counts = %d online, %d offline, %v; want 200 online and 2 offline", online, offline, err)
	}
}
