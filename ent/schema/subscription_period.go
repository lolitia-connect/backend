package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type SubscriptionPeriod struct {
	ent.Schema
}

func (SubscriptionPeriod) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "subscription_period"}}
}

func (SubscriptionPeriod) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").MaxLen(64).Immutable(),
		field.String("entitlement_id").MaxLen(64),
		field.String("transaction_key").MaxLen(255),
		field.Int64("user_subscribe_id"),
		field.Int64("order_id"),
		field.Int64("plan_id"),
		field.String("billing_interval").MaxLen(16).Default(""),
		field.Int64("traffic_limit"),
		field.Time("start_at"),
		field.Time("end_at"),
		field.Bool("traffic_reset").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
