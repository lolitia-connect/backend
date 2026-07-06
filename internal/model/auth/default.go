package auth

import (
	"context"

	"github.com/perfect-panel/server/ent"
	entauthmethod "github.com/perfect-panel/server/ent/authmethod"
)

var _ Model = (*customAuthModel)(nil)
var (
	cacheAuthIdPrefix     = "cache:auth:id:"
	cacheAuthMethodPrefix = "cache:auth:method:"
)

type (
	Model interface {
		authModel
		customAuthLogicModel
	}
	authModel interface {
		Insert(ctx context.Context, data *Auth) error
		FindOne(ctx context.Context, id int64) (*Auth, error)
		Update(ctx context.Context, data *Auth) error
		Delete(ctx context.Context, id int64) error
	}

	customAuthModel struct {
		*defaultAuthModel
	}
	defaultAuthModel struct {
		db *ent.Client
	}
)

func newAuthModel(db *ent.Client) *defaultAuthModel {
	return &defaultAuthModel{
		db: db,
	}
}

func (m *defaultAuthModel) Insert(ctx context.Context, data *Auth) error {
	created, err := m.db.AuthMethod.Create().
		SetMethod(data.Method).
		SetConfig(data.Config).
		SetEnabled(value(data.Enabled)).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, created)
	return nil
}

func (m *defaultAuthModel) FindOne(ctx context.Context, id int64) (*Auth, error) {
	data, err := m.db.AuthMethod.Query().Where(entauthmethod.ID(id)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return authFromEnt(data), nil
}

func (m *defaultAuthModel) Update(ctx context.Context, data *Auth) error {
	updated, err := m.db.AuthMethod.UpdateOneID(data.Id).
		SetMethod(data.Method).
		SetConfig(data.Config).
		SetEnabled(value(data.Enabled)).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, updated)
	return nil
}

func (m *defaultAuthModel) Delete(ctx context.Context, id int64) error {
	err := m.db.AuthMethod.DeleteOneID(id).Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

func authFromEnt(data *ent.AuthMethod) *Auth {
	if data == nil {
		return nil
	}
	var resp Auth
	copyFromEnt(&resp, data)
	return &resp
}

func copyFromEnt(dst *Auth, src *ent.AuthMethod) {
	dst.Id = src.ID
	dst.Method = src.Method
	dst.Config = src.Config
	dst.Enabled = ptr(src.Enabled)
	dst.CreatedAt = src.CreatedAt
	dst.UpdatedAt = src.UpdatedAt
}

func ptr(v bool) *bool { return &v }

func value(v *bool) bool {
	return v != nil && *v
}
