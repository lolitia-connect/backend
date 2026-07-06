package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type UserWithdrawal struct {
	ent.Schema
}

func (UserWithdrawal) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "user_withdrawal"}}
}

func (UserWithdrawal) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.Int64("user_id").StorageKey("user_id"),
		field.Int64("amount").StorageKey("amount"),
		field.String("content").StorageKey("content").Optional(),
		field.Uint8("status").StorageKey("status").Default(0),
		field.String("reason").StorageKey("reason").MaxLen(500).Default(""),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
