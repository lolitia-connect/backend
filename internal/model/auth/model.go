package auth

import (
	"context"

	"github.com/perfect-panel/server/ent"
	entauthmethod "github.com/perfect-panel/server/ent/authmethod"
)

type customAuthLogicModel interface {
	GetAuthListByPage(ctx context.Context) ([]*Auth, error)
	FindOneByMethod(ctx context.Context, platform string) (*Auth, error)
	FindAll(ctx context.Context) ([]*Auth, error)
}

// NewModel returns a model for the database table.
func NewModel(conn *ent.Client) Model {
	return &customAuthModel{
		defaultAuthModel: newAuthModel(conn),
	}
}

type Filter struct {
	Show   *bool
	Pinned *bool
	Popup  *bool
	Search string
}

// GetAuthListByPage  get auth list by page
func (m *customAuthModel) GetAuthListByPage(ctx context.Context) ([]*Auth, error) {
	return m.FindAll(ctx)
}

// FindOneByMethod  find one by method
func (m *customAuthModel) FindOneByMethod(ctx context.Context, method string) (*Auth, error) {
	data, err := m.db.AuthMethod.Query().Where(entauthmethod.Method(method)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return authFromEnt(data), nil
}

// FindAll find all
func (m *customAuthModel) FindAll(ctx context.Context) ([]*Auth, error) {
	items, err := m.db.AuthMethod.Query().All(ctx)
	if err != nil {
		return nil, err
	}
	list := make([]*Auth, 0, len(items))
	for _, item := range items {
		list = append(list, authFromEnt(item))
	}
	return list, nil
}
