package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type Server struct {
	ent.Schema
}

func (Server) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "servers"}}
}

func (Server) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").StorageKey("id").Immutable(),
		field.String("name").StorageKey("name").MaxLen(100).Default(""),
		field.String("country").StorageKey("country").MaxLen(128).Default(""),
		field.String("city").StorageKey("city").MaxLen(128).Default(""),
		field.String("address").StorageKey("address").MaxLen(100).Default(""),
		field.Int("sort").StorageKey("sort").Default(0),
		field.String("protocols").StorageKey("protocols").Optional().Nillable(),
		field.Time("last_reported_at").StorageKey("last_reported_at").Optional().Nillable(),
		field.String("longitude").StorageKey("longitude").MaxLen(50).Default("0.0"),
		field.String("latitude").StorageKey("latitude").MaxLen(50).Default("0.0"),
		field.String("longitude_center").StorageKey("longitude_center").MaxLen(50).Default("0.0"),
		field.String("latitude_center").StorageKey("latitude_center").MaxLen(50).Default("0.0"),
		field.Time("created_at").StorageKey("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").StorageKey("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
