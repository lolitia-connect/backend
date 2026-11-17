package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type SubscriptionEntitlement struct {
	ent.Schema
}

func (SubscriptionEntitlement) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "subscription_entitlement"}}
}

func (SubscriptionEntitlement) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").MaxLen(64).Immutable(),
		field.String("source").MaxLen(32),
		field.String("scope").MaxLen(255),
		field.String("subscription_key").MaxLen(255),
		field.Int64("user_id"),
		field.Int64("user_subscribe_id"),
		field.Int64("revision"),
		field.String("fingerprint").MaxLen(64),
		field.String("period_id").MaxLen(64),
		field.String("mode").MaxLen(32),
		field.String("status").MaxLen(32),
		field.Bool("auto_renew").Default(false),
		field.Time("grace_until").Optional().Nillable(),
		field.Time("revoked_at").Optional().Nillable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (SubscriptionEntitlement) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_subscribe_id").Unique()}
}
