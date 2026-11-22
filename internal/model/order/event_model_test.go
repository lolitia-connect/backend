package order

import (
	"database/sql"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	_ "modernc.org/sqlite"
)

func newEventTestClient(t *testing.T) *ent.Client {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
}

func TestEventModelPersistsReplayAndPublicationState(t *testing.T) {
	ctx := t.Context()
	client := newEventTestClient(t)
	model := NewEventModel(client)
	if err := model.Insert(ctx, nil); err != nil {
		t.Fatal(err)
	}
	old := &Event{OrderId: 17, OrderNo: "order-17", EventType: EventOrderCreated, Payload: `{"state_version":1}`}
	paid := &Event{OrderId: 17, OrderNo: "order-17", EventType: EventOrderPaymentPaid, Payload: `{"state_version":2}`}
	other := &Event{OrderId: 18, OrderNo: "order-18", EventType: EventOrderCreated, Payload: `{"state_version":1}`}
	for _, event := range []*Event{old, paid, other} {
		if err := model.Insert(ctx, event); err != nil {
			t.Fatal(err)
		}
		if event.Id == 0 || event.CreatedAt.IsZero() {
			t.Fatalf("insert did not hydrate event: %+v", event)
		}
	}
	found, err := model.FindOne(ctx, paid.Id)
	if err != nil || found.Payload != paid.Payload || found.PublishedAt != nil {
		t.Fatalf("FindOne = %+v, %v", found, err)
	}
	paidAt, err := model.PaidAt(ctx, paid.OrderNo)
	if err != nil || !paidAt.Equal(paid.CreatedAt) {
		t.Fatalf("PaidAt = %v, %v", paidAt, err)
	}
	if earliest, err := model.EarliestID(ctx, "missing"); err != nil || earliest != 0 {
		t.Fatalf("empty EarliestID = %d, %v", earliest, err)
	}
	if earliest, err := model.EarliestID(ctx, old.OrderNo); err != nil || earliest != old.Id {
		t.Fatalf("EarliestID = %d, %v, want %d", earliest, err, old.Id)
	}
	replay, err := model.ListAfter(ctx, old.OrderNo, old.Id, 0)
	if err != nil || len(replay) != 1 || replay[0].Id != paid.Id {
		t.Fatalf("ListAfter = %+v, %v", replay, err)
	}
	pending, err := model.ListUnpublished(ctx, 2)
	if err != nil || len(pending) != 2 || pending[0].Id != old.Id || pending[1].Id != paid.Id {
		t.Fatalf("ListUnpublished = %+v, %v", pending, err)
	}
	publishedAt := time.Now().UTC()
	if ok, err := model.MarkPublished(ctx, paid.Id, publishedAt); err != nil || !ok {
		t.Fatalf("first MarkPublished = %t, %v", ok, err)
	}
	if ok, err := model.MarkPublished(ctx, paid.Id, publishedAt.Add(time.Minute)); err != nil || ok {
		t.Fatalf("duplicate MarkPublished = %t, %v", ok, err)
	}
	if ok, err := model.MarkPublished(ctx, 999999, publishedAt); err != nil || ok {
		t.Fatalf("missing MarkPublished = %t, %v", ok, err)
	}
	if got, err := model.ListUnpublished(ctx, 1001); err != nil || len(got) != 2 || got[0].Id != old.Id || got[1].Id != other.Id {
		t.Fatalf("published event remained pending: %+v, %v", got, err)
	}
	published, err := model.FindOne(ctx, paid.Id)
	if err != nil || published.PublishedAt == nil || !published.PublishedAt.Equal(publishedAt) {
		t.Fatalf("publication timestamp changed after retry: %+v, %v", published, err)
	}
	if deleted, err := model.DeletePublishedBefore(ctx, paid.CreatedAt); err != nil || deleted != 0 {
		t.Fatalf("retention cutoff deleted an event at the boundary: %d, %v", deleted, err)
	}
	cutoff := paid.CreatedAt.Add(time.Hour)
	deleted, err := model.DeletePublishedBefore(ctx, cutoff)
	if err != nil || deleted != 1 {
		t.Fatalf("DeletePublishedBefore = %d, %v", deleted, err)
	}
	if _, err := model.FindOne(ctx, paid.Id); err == nil {
		t.Fatal("published event was not deleted")
	}
	if _, err := model.FindOne(ctx, old.Id); err != nil {
		t.Fatalf("unpublished event was deleted: %v", err)
	}
}
