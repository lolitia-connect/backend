package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type SubscriptionEntitlementRevision struct {
	ent.Schema
}

func (SubscriptionEntitlementRevision) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "subscription_entitlement_revision"}}
}

func (SubscriptionEntitlementRevision) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").MaxLen(96).Immutable(),
		field.String("entitlement_id").MaxLen(64),
		field.Text("payload"),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}
