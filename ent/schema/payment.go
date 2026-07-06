package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Payment holds the schema definition for the payment table.
type Payment struct {
	ent.Schema
}

func (Payment) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "payment"},
	}
}

func (Payment) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.String("name").StorageKey("name").Default(""),
		field.String("platform").StorageKey("platform"),
		field.String("icon").StorageKey("icon").Default(""),
		field.String("domain").StorageKey("domain").Default(""),
		field.String("config").StorageKey("config"),
		field.String("description").StorageKey("description").Optional(),
		field.Uint("fee_mode").StorageKey("fee_mode").Default(0),
		field.Int64("fee_percent").StorageKey("fee_percent").Default(0),
		field.Int64("fee_amount").StorageKey("fee_amount").Default(0),
		field.Int64("sort").StorageKey("sort").Default(0),
		field.Bool("enable").StorageKey("enable").Default(false),
		field.String("token").StorageKey("token").Default(""),
		field.String("currency_unit").StorageKey("currency_unit").Default(""),
		field.Float("exchange_rate").StorageKey("exchange_rate").Default(0),
		field.String("bill_desc").StorageKey("bill_desc").Default(""),
	}
}

func (Payment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("token").Unique(),
	}
}
