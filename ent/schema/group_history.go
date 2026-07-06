package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type GroupHistory struct {
	ent.Schema
}

func (GroupHistory) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "group_history"}}
}

func (GroupHistory) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.String("group_mode").StorageKey("group_mode").MaxLen(50),
		field.String("trigger_type").StorageKey("trigger_type").MaxLen(50),
		field.String("state").StorageKey("state").MaxLen(50),
		field.Int("total_users").StorageKey("total_users").Default(0),
		field.Int("success_count").StorageKey("success_count").Default(0),
		field.Int("failed_count").StorageKey("failed_count").Default(0),
		field.Time("start_time").StorageKey("start_time").Optional().Nillable(),
		field.Time("end_time").StorageKey("end_time").Optional().Nillable(),
		field.String("operator").StorageKey("operator").MaxLen(100).Optional(),
		field.String("error_message").StorageKey("error_message").Optional(),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
	}
}
