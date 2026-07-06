package document

import (
	"context"

	"github.com/perfect-panel/server/ent"
	entdocument "github.com/perfect-panel/server/ent/document"
)

var _ Model = (*customDocumentModel)(nil)
var (
	cacheDocumentIdPrefix = "cache:document:id:"
)

type (
	Model interface {
		documentModel
		customDocumentLogicModel
	}
	documentModel interface {
		Insert(ctx context.Context, data *Document) error
		FindOne(ctx context.Context, id int64) (*Document, error)
		Update(ctx context.Context, data *Document) error
		Delete(ctx context.Context, id int64) error
	}

	customDocumentModel struct {
		*defaultDocumentModel
	}
	defaultDocumentModel struct {
		db *ent.Client
	}
)

func newDocumentModel(db *ent.Client) *defaultDocumentModel {
	return &defaultDocumentModel{
		db: db,
	}
}

func (m *defaultDocumentModel) Insert(ctx context.Context, data *Document) error {
	created, err := m.db.Document.Create().
		SetTitle(data.Title).
		SetContent(data.Content).
		SetTags(data.Tags).
		SetShow(value(data.Show)).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, created)
	return nil
}

func (m *defaultDocumentModel) FindOne(ctx context.Context, id int64) (*Document, error) {
	data, err := m.db.Document.Query().Where(entdocument.ID(id)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return documentFromEnt(data), nil
}

func (m *defaultDocumentModel) Update(ctx context.Context, data *Document) error {
	updated, err := m.db.Document.UpdateOneID(data.Id).
		SetTitle(data.Title).
		SetContent(data.Content).
		SetTags(data.Tags).
		SetShow(value(data.Show)).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, updated)
	return nil
}

func (m *defaultDocumentModel) Delete(ctx context.Context, id int64) error {
	err := m.db.Document.DeleteOneID(id).Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

func documentFromEnt(data *ent.Document) *Document {
	if data == nil {
		return nil
	}
	var resp Document
	copyFromEnt(&resp, data)
	return &resp
}

func copyFromEnt(dst *Document, src *ent.Document) {
	dst.Id = src.ID
	dst.Title = src.Title
	dst.Content = src.Content
	dst.Tags = src.Tags
	dst.Show = ptr(src.Show)
	dst.CreatedAt = src.CreatedAt
	dst.UpdatedAt = src.UpdatedAt
}

func ptr(v bool) *bool { return &v }

func value(v *bool) bool {
	return v == nil || *v
}
