package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type Node struct {
	ent.Schema
}

func (Node) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "nodes"}}
}

func (Node) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.String("name").StorageKey("name").MaxLen(100).Default(""),
		field.String("tags").StorageKey("tags").MaxLen(255).Default(""),
		field.Uint16("port").StorageKey("port").Default(0),
		field.String("address").StorageKey("address").MaxLen(255).Default(""),
		field.Int64("server_id").StorageKey("server_id").Default(0),
		field.String("protocol").StorageKey("protocol").MaxLen(100).Default(""),
		field.String("protocol_id").StorageKey("protocol_id").MaxLen(100).Default(""),
		field.Bool("enabled").StorageKey("enabled").Default(true),
		field.String("node_type").StorageKey("node_type").MaxLen(20).Default("landing"),
		field.Bool("is_hidden").StorageKey("is_hidden").Default(false),
		field.Int("sort").StorageKey("sort").Default(0),
		field.JSON("node_group_ids", []int64{}).StorageKey("node_group_ids").Optional(),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
