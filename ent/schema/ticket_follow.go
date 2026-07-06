package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// TicketFollow holds the schema definition for the ticket_follow table.
type TicketFollow struct {
	ent.Schema
}

func (TicketFollow) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "ticket_follow"},
	}
}

func (TicketFollow) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.Int64("ticket_id").StorageKey("ticket_id").Default(0),
		field.String("from").StorageKey("from").Default(""),
		field.Uint8("type").StorageKey("type").Default(1),
		field.String("content").StorageKey("content").Optional(),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
	}
}
