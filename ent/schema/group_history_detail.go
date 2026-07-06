package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type GroupHistoryDetail struct {
	ent.Schema
}

func (GroupHistoryDetail) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "group_history_detail"}}
}

func (GroupHistoryDetail) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.Int64("history_id").StorageKey("history_id"),
		field.Int64("node_group_id").StorageKey("node_group_id"),
		field.Int("user_count").StorageKey("user_count").Default(0),
		field.Int("node_count").StorageKey("node_count").Default(0),
		field.String("user_data").StorageKey("user_data").Optional(),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
	}
}
