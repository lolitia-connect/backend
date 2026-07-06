package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type RedemptionCode struct {
	ent.Schema
}

func (RedemptionCode) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("code").Default(""),
		field.Int64("total_count").Default(0),
		field.Int64("used_count").Default(0),
		field.Int64("subscribe_plan").Default(0),
		field.String("unit_time").Default("month"),
		field.Int64("quantity").Default(1),
		field.Int64("status").Default(1),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		field.Time("deleted_at").Optional().Nillable(),
	}
}

func (RedemptionCode) Edges() []ent.Edge {
	return nil
}

func (RedemptionCode) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "redemption_code"},
	}
}
