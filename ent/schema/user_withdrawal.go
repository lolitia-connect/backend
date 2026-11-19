package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserWithdrawal holds the commission-withdrawal requests an administrator
// reviews. The physical table is `withdrawals`: migration 02121 creates that
// name and upstream's GORM entity maps to it too, so the annotation has to
// agree with the migration rather than with the ent type name.
type UserWithdrawal struct {
	ent.Schema
}

func (UserWithdrawal) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "withdrawals"}}
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

// Indexes mirrors the KEY idx_user_id that migration 02121 creates, so the
// declared schema and the migrated schema do not drift apart.
func (UserWithdrawal) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
	}
}
