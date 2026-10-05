package order

import (
	"context"
	"time"

	"github.com/perfect-panel/server/ent"
	entorderevent "github.com/perfect-panel/server/ent/orderevent"
)

var _ EventModel = (*defaultEventModel)(nil)

type defaultEventModel struct {
	db *ent.Client
}

func newEventModel(db *ent.Client) *defaultEventModel {
	return &defaultEventModel{db: db}
}

func (m *defaultEventModel) Insert(ctx context.Context, event *Event) error {
	if event == nil {
		return nil
	}
	saved, err := m.db.OrderEvent.Create().
		SetOrderID(event.OrderId).
		SetOrderNo(event.OrderNo).
		SetEventType(event.EventType).
		SetPayload(event.Payload).
		Save(ctx)
	if err != nil {
		return err
	}
	*event = *entToEvent(saved)
	return nil
}

func (m *defaultEventModel) FindOne(ctx context.Context, id int64) (*Event, error) {
	data, err := m.db.OrderEvent.Get(ctx, id)
	return entToEvent(data), err
}

func (m *defaultEventModel) ListAfter(ctx context.Context, orderNo string, afterId int64, limit int) ([]*Event, error) {
	list, err := m.db.OrderEvent.Query().
		Where(
			entorderevent.OrderNo(orderNo),
			entorderevent.IDGT(afterId),
		).
		Order(entorderevent.ByID()).
		Limit(normalizeEventLimit(limit)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return entEventsToEvents(list), nil
}

func (m *defaultEventModel) EarliestID(ctx context.Context, orderNo string) (int64, error) {
	earliest, err := m.db.OrderEvent.Query().
		Where(entorderevent.OrderNo(orderNo)).
		Order(entorderevent.ByID()).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return 0, nil
		}
		return 0, err
	}
	return earliest.ID, nil
}

func (m *defaultEventModel) ListUnpublished(ctx context.Context, limit int) ([]*Event, error) {
	list, err := m.db.OrderEvent.Query().
		Where(entorderevent.PublishedAtIsNil()).
		Order(entorderevent.ByID()).
		Limit(normalizeEventLimit(limit)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return entEventsToEvents(list), nil
}

func (m *defaultEventModel) MarkPublished(ctx context.Context, id int64, publishedAt time.Time) (bool, error) {
	affected, err := m.db.OrderEvent.Update().
		Where(
			entorderevent.ID(id),
			entorderevent.PublishedAtIsNil(),
		).
		SetPublishedAt(publishedAt).
		Save(ctx)
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func (m *defaultEventModel) DeletePublishedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	affected, err := m.db.OrderEvent.Delete().
		Where(
			entorderevent.PublishedAtNotNil(),
			entorderevent.CreatedAtLT(cutoff),
		).
		Exec(ctx)
	return int64(affected), err
}
