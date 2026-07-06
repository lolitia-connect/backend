package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// Order holds the schema definition for the order table.
type Order struct {
	ent.Schema
}

func (Order) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "order"},
	}
}

func (Order) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.Int64("parent_id").StorageKey("parent_id").Default(0),
		field.Int64("user_id").StorageKey("user_id").Default(0),
		field.String("order_no").StorageKey("order_no").MaxLen(255).Default(""),
		field.Uint8("type").StorageKey("type").Default(1),
		field.Int64("quantity").StorageKey("quantity").Default(1),
		field.Int64("price").StorageKey("price").Default(0),
		field.Int64("amount").StorageKey("amount").Default(0),
		field.Int64("discount").StorageKey("discount").Default(0),
		field.String("coupon").StorageKey("coupon").MaxLen(255).Optional().Nillable(),
		field.Int64("coupon_discount").StorageKey("coupon_discount").Default(0),
		field.Int64("payment_id").StorageKey("payment_id").Default(0),
		field.String("method").StorageKey("method").MaxLen(255).Default(""),
		field.Int64("fee_amount").StorageKey("fee_amount").Default(0),
		field.String("trade_no").StorageKey("trade_no").MaxLen(255).Optional().Nillable(),
		field.Int64("gift_amount").StorageKey("gift_amount").Default(0),
		field.Int64("commission").StorageKey("commission").Default(0),
		field.Uint8("status").StorageKey("status").Default(1),
		field.Int64("subscribe_id").StorageKey("subscribe_id").Default(0),
		field.String("subscribe_token").StorageKey("subscribe_token").MaxLen(255).Optional().Nillable(),
		field.Bool("is_new").StorageKey("is_new").Default(false),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
