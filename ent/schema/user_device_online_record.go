package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type UserDeviceOnlineRecord struct {
	ent.Schema
}

func (UserDeviceOnlineRecord) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "user_device_online_record"}}
}

func (UserDeviceOnlineRecord) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.Int64("user_id").StorageKey("user_id"),
		field.String("identifier").StorageKey("identifier").MaxLen(255),
		field.Time("online_time").StorageKey("online_time"),
		field.Time("offline_time").StorageKey("offline_time"),
		field.Int64("online_seconds").StorageKey("online_seconds").Default(0),
		field.Int64("duration_days").StorageKey("duration_days").Default(0),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
	}
}
