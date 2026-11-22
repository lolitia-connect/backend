package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type NekoproxyBinding struct {
	ent.Schema
}

func (NekoproxyBinding) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "nekoproxy_bindings"}}
}

func (NekoproxyBinding) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").Immutable(),
		field.Int64("node_id").Unique().Immutable(),
		field.String("api_url").MaxLen(512).NotEmpty(),
		field.String("forward_id").MaxLen(128).NotEmpty(),
		field.String("session_id").MaxLen(128).NotEmpty(),
		field.Bool("sync_on_save").Default(true),
		field.Uint16("managed_target_port").Optional().Nillable().Comment("Last protocol-managed target port; 0 is manual, NULL is an unclassified binding"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (NekoproxyBinding) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("node", Node.Type).Ref("nekoproxy_binding").Field("node_id").Unique().Required().Immutable(),
	}
}

func (NekoproxyBinding) Indexes() []ent.Index {
	return []ent.Index{index.Fields("api_url", "forward_id").Unique()}
}
