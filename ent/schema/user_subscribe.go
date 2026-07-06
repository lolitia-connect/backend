package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// UserSubscribe holds the schema definition for the user_subscribe table.
type UserSubscribe struct {
	ent.Schema
}

func (UserSubscribe) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "user_subscribe"},
	}
}

func (UserSubscribe) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.Int64("user_id").StorageKey("user_id"),
		field.Int64("order_id").StorageKey("order_id"),
		field.Int64("subscribe_id").StorageKey("subscribe_id"),
		field.Int64("node_group_id").StorageKey("node_group_id").Default(0),
		field.Bool("group_locked").StorageKey("group_locked").Default(false),
		field.Time("start_time").StorageKey("start_time").Default(time.Now),
		field.Time("expire_time").StorageKey("expire_time").Optional(),
		field.Time("finished_at").StorageKey("finished_at").Optional().Nillable(),
		field.Int64("traffic").StorageKey("traffic").Default(0),
		field.Bool("traffic_unlimited").StorageKey("traffic_unlimited").Default(false),
		field.Int64("download").StorageKey("download").Default(0),
		field.Int64("upload").StorageKey("upload").Default(0),
		field.Int64("expired_download").StorageKey("expired_download").Default(0),
		field.Int64("expired_upload").StorageKey("expired_upload").Default(0),
		field.String("token").StorageKey("token").MaxLen(255).Default(""),
		field.String("uuid").StorageKey("uuid").MaxLen(255).Default(""),
		field.Uint8("status").StorageKey("status").Default(0),
		field.String("note").StorageKey("note").MaxLen(500).Default(""),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
