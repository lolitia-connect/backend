package document

import (
	"context"
	"strings"

	"github.com/perfect-panel/server/ent"
	entdocument "github.com/perfect-panel/server/ent/document"
)

type customDocumentLogicModel interface {
	QueryDocumentDetail(ctx context.Context, id int64) (*Document, error)
	QueryDocumentList(ctx context.Context, page, size int, tag string, search string) (int64, []*Document, error)
	GetDocumentListByAll(ctx context.Context) (int64, []*Document, error)
}

// NewModel returns a model for the database table.
func NewModel(conn *ent.Client) Model {
	return &customDocumentModel{
		defaultDocumentModel: newDocumentModel(conn),
	}
}

// QueryDocumentDetail queries the details of a document.
func (m *customDocumentModel) QueryDocumentDetail(ctx context.Context, id int64) (*Document, error) {
	return m.FindOne(ctx, id)
}

// QueryDocumentList queries a list of documents.
func (m *customDocumentModel) QueryDocumentList(ctx context.Context, page, size int, tag string, search string) (int64, []*Document, error) {
	query := m.db.Document.Query()
	if tag != "" {
		query = query.Where(entdocument.Or(entdocument.Tags(tag), entdocument.TagsHasPrefix(tag+","), entdocument.TagsHasSuffix(","+tag), entdocument.TagsContains(","+tag+",")))
	}
	if search != "" {
		query = query.Where(entdocument.Or(entdocument.TitleContains(search), entdocument.ContentContains(search)))
	}
	total, err := query.Count(ctx)
	if err != nil {
		return 0, nil, err
	}
	items, err := query.Offset((page - 1) * size).Limit(size).All(ctx)
	if err != nil {
		return 0, nil, err
	}
	data := make([]*Document, 0, len(items))
	for _, item := range items {
		data = append(data, documentFromEnt(item))
	}
	return int64(total), data, nil
}

// GetDocumentListByAll queries a list of documents.
func (m *customDocumentModel) GetDocumentListByAll(ctx context.Context) (int64, []*Document, error) {
	items, err := m.db.Document.Query().Where(entdocument.Show(true)).All(ctx)
	if err != nil {
		return 0, nil, err
	}
	data := make([]*Document, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Title) == "" && strings.TrimSpace(item.Content) == "" {
			continue
		}
		data = append(data, documentFromEnt(item))
	}
	return int64(len(data)), data, nil
}
