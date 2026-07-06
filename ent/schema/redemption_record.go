package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type RedemptionRecord struct {
	ent.Schema
}

func (RedemptionRecord) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("redemption_code_id").Default(0),
		field.Int64("user_id").Default(0),
		field.Int64("subscribe_id").Default(0),
		field.String("unit_time").Default("month"),
		field.Int64("quantity").Default(1),
		field.Time("redeemed_at").Default(time.Now),
		field.Time("created_at").Default(time.Now),
	}
}

func (RedemptionRecord) Edges() []ent.Edge {
	return nil
}

func (RedemptionRecord) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "redemption_record"},
	}
}
