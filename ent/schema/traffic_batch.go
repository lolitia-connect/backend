package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type TrafficBatch struct {
	ent.Schema
}

func (TrafficBatch) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "traffic_batch"}}
}

func (TrafficBatch) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").MaxLen(96).Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}
