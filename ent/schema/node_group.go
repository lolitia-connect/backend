package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type NodeGroup struct {
	ent.Schema
}

func (NodeGroup) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "node_group"}}
}

func (NodeGroup) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.String("name").StorageKey("name").MaxLen(255),
		field.String("type").StorageKey("group_type").MaxLen(32).Default("common"),
		field.String("description").StorageKey("description").MaxLen(500).Optional(),
		field.Int("sort").StorageKey("sort").Default(0),
		field.Bool("for_calculation").StorageKey("for_calculation").Default(true),
		field.Bool("is_expired_group").StorageKey("is_expired_group").Default(false),
		field.Int("expired_days_limit").StorageKey("expired_days_limit").Default(7),
		field.Int64("max_traffic_gb_expired").StorageKey("max_traffic_gb_expired").Optional().Nillable(),
		field.Int("speed_limit").StorageKey("speed_limit").Default(0),
		field.Int64("min_traffic_gb").StorageKey("min_traffic_gb").Optional().Nillable(),
		field.Int64("max_traffic_gb").StorageKey("max_traffic_gb").Optional().Nillable(),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
