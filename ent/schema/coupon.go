package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Coupon holds the schema definition for the coupon table.
type Coupon struct {
	ent.Schema
}

func (Coupon) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "coupon"},
	}
}

func (Coupon) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.String("name").StorageKey("name").Default(""),
		field.String("code").StorageKey("code").Default(""),
		field.Int64("count").StorageKey("count").Default(0),
		field.Uint8("type").StorageKey("type").Default(1),
		field.Int64("discount").StorageKey("discount").Default(0),
		field.Int64("start_time").StorageKey("start_time").Default(0),
		field.Int64("expire_time").StorageKey("expire_time").Default(0),
		field.Int64("user_limit").StorageKey("user_limit").Default(0),
		field.String("subscribe").StorageKey("subscribe").Default(""),
		field.Int64("used_count").StorageKey("used_count").Default(0),
		field.Bool("enable").StorageKey("enable").Default(true),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Coupon) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("code").Unique(),
	}
}
