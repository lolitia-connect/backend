package orderLogic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	orderModel "github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/orderstream"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
)

// fakeEventModel is an in-memory outbox. The publisher only needs the ordered
// unpublished read plus a conditional mark, which is exactly the contract the
// ent model implements.
type fakeEventModel struct {
	unpublished []*orderModel.Event
	marked      []int64
	cutoff      time.Time
	deleted     int64
	publishErr  error
	markErr     error
}

func (f *fakeEventModel) Insert(context.Context, *orderModel.Event) error { return nil }

func (f *fakeEventModel) FindOne(context.Context, int64) (*orderModel.Event, error) { return nil, nil }

func (f *fakeEventModel) ListAfter(context.Context, string, int64, int) ([]*orderModel.Event, error) {
	return nil, nil
}

func (f *fakeEventModel) EarliestID(context.Context, string) (int64, error) { return 0, nil }

func (f *fakeEventModel) ListUnpublished(context.Context, int) ([]*orderModel.Event, error) {
	if f.publishErr != nil {
		return nil, f.publishErr
	}
	return f.unpublished, nil
}

func (f *fakeEventModel) MarkPublished(_ context.Context, id int64, _ time.Time) (bool, error) {
	if f.markErr != nil {
		return false, f.markErr
	}
	f.marked = append(f.marked, id)
	return true, nil
}

func (f *fakeEventModel) DeletePublishedBefore(_ context.Context, cutoff time.Time) (int64, error) {
	f.cutoff = cutoff
	return f.deleted, nil
}

// fakeStore embeds the full Store interface so only the outbox accessor has to
// be supplied.
type fakeStore struct {
	repository.Store
	events orderModel.EventModel
}

func (f *fakeStore) OrderEvent() orderModel.EventModel { return f.events }

func newPublishTestContext(t *testing.T, events orderModel.EventModel) (*svc.ServiceContext, *redis.Client, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &svc.ServiceContext{Store: &fakeStore{events: events}, Redis: client}, client, server
}

func TestPublishOrderEventsBroadcastsTheEventIDThenMarksItPublished(t *testing.T) {
	events := &fakeEventModel{unpublished: []*orderModel.Event{
		{Id: 41, OrderNo: "order-a", EventType: orderModel.EventOrderCreated},
		{Id: 42, OrderNo: "order-a", EventType: orderModel.EventOrderPaymentPaid},
	}}
	svcCtx, client, _ := newPublishTestContext(t, events)

	pubsub := client.Subscribe(context.Background(), orderstream.Channel("order-a"))
	t.Cleanup(func() { _ = pubsub.Close() })
	if _, err := pubsub.Receive(context.Background()); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	logic := NewPublishOrderEventsLogic(svcCtx)
	if err := logic.ProcessTask(context.Background(), asynq.NewTask("test", nil)); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}

	// The message is only a wake-up hint: subscribers re-read the durable row,
	// so the payload must be the event id and nothing else.
	waited := make(map[string]bool, 2)
	timeout := time.After(2 * time.Second)
	for len(waited) < 2 {
		select {
		case message := <-pubsub.Channel():
			waited[message.Payload] = true
		case <-timeout:
			t.Fatalf("received %v, want both event ids", waited)
		}
	}
	if !waited["41"] || !waited["42"] {
		t.Fatalf("published payloads = %v, want 41 and 42", waited)
	}
	if len(events.marked) != 2 || events.marked[0] != 41 || events.marked[1] != 42 {
		t.Fatalf("marked published = %v, want [41 42] in order", events.marked)
	}
}

func TestPublishOrderEventsStopsAtTheFirstRedisFailure(t *testing.T) {
	events := &fakeEventModel{unpublished: []*orderModel.Event{
		{Id: 1, OrderNo: "order-a"},
		{Id: 2, OrderNo: "order-a"},
	}}
	svcCtx, client, server := newPublishTestContext(t, events)
	server.Close()

	logic := NewPublishOrderEventsLogic(svcCtx)
	if err := logic.ProcessTask(context.Background(), asynq.NewTask("test", nil)); err == nil {
		t.Fatal("a Redis outage must surface as an error so the task retries")
	}
	// Nothing may be marked published on a path that did not reach Redis.
	if len(events.marked) != 0 {
		t.Fatalf("marked published = %v, want none", events.marked)
	}
	_ = client
}

func TestPublishOrderEventsSurfacesAReadFailure(t *testing.T) {
	events := &fakeEventModel{publishErr: errors.New("outbox unavailable")}
	svcCtx, _, _ := newPublishTestContext(t, events)

	logic := NewPublishOrderEventsLogic(svcCtx)
	if err := logic.ProcessTask(context.Background(), asynq.NewTask("test", nil)); err == nil {
		t.Fatal("an unreadable outbox must be retried, not reported as success")
	}
}

func TestPublishOrderEventsIsANoOpOnAnEmptyBacklog(t *testing.T) {
	events := &fakeEventModel{}
	svcCtx, _, _ := newPublishTestContext(t, events)

	logic := NewPublishOrderEventsLogic(svcCtx)
	if err := logic.ProcessTask(context.Background(), asynq.NewTask("test", nil)); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	if len(events.marked) != 0 {
		t.Fatalf("marked published = %v, want none", events.marked)
	}
}

func TestCleanupOrderEventsUsesTheReplayRetentionWindow(t *testing.T) {
	events := &fakeEventModel{deleted: 3}
	svcCtx, _, _ := newPublishTestContext(t, events)

	logic := NewCleanupOrderEventsLogic(svcCtx)
	if err := logic.ProcessTask(context.Background(), asynq.NewTask("test", nil)); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}

	// The model deletes only rows that are both published and older than the
	// cutoff, so the cutoff is the whole contract of this task: it must sit one
	// replay window in the past, never "now".
	want := time.Now().Add(-orderEventRetention)
	if events.cutoff.IsZero() {
		t.Fatal("cleanup did not query the outbox")
	}
	if diff := events.cutoff.Sub(want); diff > time.Minute || diff < -time.Minute {
		t.Fatalf("cutoff = %v, want about %v (off by %v)", events.cutoff, want, diff)
	}
	if orderEventRetention != 30*24*time.Hour {
		t.Fatalf("retention = %v, want 30 days", orderEventRetention)
	}
}
