package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OrderEvent is the durable transactional outbox for order state changes.
// Redis Pub/Sub only distributes these rows with low latency; a reconnecting
// client always recovers its state by reading this table, so an event is only
// ever written in the same transaction as the state change it announces.
type OrderEvent struct {
	ent.Schema
}

func (OrderEvent) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "order_event"},
	}
}

func (OrderEvent) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.Int64("order_id").StorageKey("order_id").Default(0),
		field.String("order_no").StorageKey("order_no").MaxLen(255).Default(""),
		field.String("event_type").StorageKey("event_type").MaxLen(64).Default(""),
		field.String("payload").StorageKey("payload"),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("published_at").StorageKey("published_at").Optional().Nillable(),
	}
}

func (OrderEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("order_id", "id"),
		index.Fields("order_no", "id"),
		index.Fields("published_at", "id"),
	}
}
