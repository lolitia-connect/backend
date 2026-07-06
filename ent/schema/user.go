package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// User holds the schema definition for the user table.
type User struct {
	ent.Schema
}

func (User) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "user"},
	}
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.String("password").StorageKey("password").MaxLen(100),
		field.String("algo").StorageKey("algo").MaxLen(20).Default("default"),
		field.String("salt").StorageKey("salt").MaxLen(20).Optional().Nillable(),
		field.String("avatar").StorageKey("avatar").Optional(),
		field.Int64("balance").StorageKey("balance").Default(0),
		field.String("refer_code").StorageKey("refer_code").MaxLen(20).Default(""),
		field.Int64("referer_id").StorageKey("referer_id").Default(0),
		field.Int64("commission").StorageKey("commission").Default(0),
		field.Uint8("referral_percentage").StorageKey("referral_percentage").Default(0),
		field.Bool("only_first_purchase").StorageKey("only_first_purchase").Default(true),
		field.Int64("gift_amount").StorageKey("gift_amount").Default(0),
		field.Bool("enable").StorageKey("enable").Default(true),
		field.Bool("is_admin").StorageKey("is_admin").Default(false),
		field.Bool("enable_balance_notify").StorageKey("enable_balance_notify").Default(false),
		field.Bool("enable_login_notify").StorageKey("enable_login_notify").Default(false),
		field.Bool("enable_subscribe_notify").StorageKey("enable_subscribe_notify").Default(false),
		field.Bool("enable_trade_notify").StorageKey("enable_trade_notify").Default(false),
		field.String("rules").StorageKey("rules").Optional(),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
		field.Time("deleted_at").StorageKey("deleted_at").Optional().Nillable(),
	}
}
