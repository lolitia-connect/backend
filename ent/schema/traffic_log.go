package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type TrafficLog struct {
	ent.Schema
}

func (TrafficLog) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "traffic_log"}}
}

func (TrafficLog) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.Int64("server_id").StorageKey("server_id"),
		field.Int64("user_id").StorageKey("user_id"),
		field.Int64("subscribe_id").StorageKey("subscribe_id"),
		field.Int64("download").StorageKey("download").Default(0),
		field.Int64("upload").StorageKey("upload").Default(0),
		field.Time("timestamp").StorageKey("timestamp").Default(time.Now),
	}
}
