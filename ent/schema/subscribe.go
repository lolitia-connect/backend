package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type Subscribe struct {
	ent.Schema
}

func (Subscribe) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "subscribe"}}
}

func (Subscribe) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.String("name").StorageKey("name").MaxLen(255).Default(""),
		field.String("language").StorageKey("language").MaxLen(255).Default(""),
		field.String("description").StorageKey("description").Optional(),
		field.Int64("unit_price").StorageKey("unit_price").Default(0),
		field.String("unit_time").StorageKey("unit_time").MaxLen(255).Default(""),
		field.String("discount").StorageKey("discount").Optional(),
		field.Int64("replacement").StorageKey("replacement").Default(0),
		field.Int64("inventory").StorageKey("inventory").Default(-1),
		field.Int64("traffic").StorageKey("traffic").Default(0),
		field.Bool("traffic_unlimited").StorageKey("traffic_unlimited").Default(false),
		field.Int64("speed_limit").StorageKey("speed_limit").Default(0),
		field.Int64("device_limit").StorageKey("device_limit").Default(0),
		field.Int64("quota").StorageKey("quota").Default(0),
		field.String("nodes").StorageKey("nodes").MaxLen(255).Optional(),
		field.String("node_tags").StorageKey("node_tags").MaxLen(255).Optional(),
		field.JSON("node_group_ids", []int64{}).StorageKey("node_group_ids").Optional(),
		field.Int64("node_group_id").StorageKey("node_group_id").Default(0),
		field.String("traffic_limit").StorageKey("traffic_limit").Optional(),
		field.Bool("show").StorageKey("show").Default(false),
		field.Bool("sell").StorageKey("sell").Default(false),
		field.Int64("sort").StorageKey("sort").Default(0),
		field.Int64("deduction_ratio").StorageKey("deduction_ratio").Default(0),
		field.Bool("allow_deduction").StorageKey("allow_deduction").Default(true),
		field.Int64("reset_cycle").StorageKey("reset_cycle").Default(0),
		field.Bool("renewal_reset").StorageKey("renewal_reset").Default(false),
		field.Bool("show_original_price").StorageKey("show_original_price").Default(true),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
