package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// UserWallet holds the billing-owned money columns that used to live on the
// identity-owned user row. One row per user; every balance, gift and
// commission movement goes through it.
type UserWallet struct {
	ent.Schema
}

func (UserWallet) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "user_wallet"},
	}
}

func (UserWallet) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.Int64("user_id").StorageKey("user_id").Immutable(),
		field.Int64("balance").StorageKey("balance").Default(0),
		field.Int64("gift_amount").StorageKey("gift_amount").Default(0),
		field.Int64("commission").StorageKey("commission").Default(0),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
