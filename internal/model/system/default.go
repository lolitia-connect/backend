package system

import (
	"context"

	"github.com/perfect-panel/server/ent"
	entsystem "github.com/perfect-panel/server/ent/system"
)

var (
	cacheSystemIdPrefix  = "cache:System:id:"
	cacheSystemKeyPrefix = "cache:System:key:"
)
var _ Model = (*customSystemModel)(nil)

type (
	Model interface {
		systemModel
		customSystemLogicModel
	}
	systemModel interface {
		Insert(ctx context.Context, data *System) error
		FindOne(ctx context.Context, id int64) (*System, error)
		FindOneByKey(ctx context.Context, email string) (*System, error)
		Update(ctx context.Context, data *System) error
		Delete(ctx context.Context, id int64) error
	}

	customSystemModel struct {
		*defaultSystemModel
	}
	defaultSystemModel struct {
		db *ent.Client
	}
)

func newSystemModel(db *ent.Client) *defaultSystemModel {
	return &defaultSystemModel{
		db: db,
	}
}

func (m *defaultSystemModel) FindOneByKey(ctx context.Context, key string) (*System, error) {
	data, err := m.db.System.Query().Where(entsystem.Key(key)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return systemFromEnt(data), nil
}

func (m *defaultSystemModel) Insert(ctx context.Context, data *System) error {
	created, err := m.db.System.Create().
		SetCategory(data.Category).
		SetKey(data.Key).
		SetValue(data.Value).
		SetType(data.Type).
		SetDesc(data.Desc).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, created)
	return nil
}

func (m *defaultSystemModel) FindOne(ctx context.Context, id int64) (*System, error) {
	data, err := m.db.System.Query().Where(entsystem.ID(id)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return systemFromEnt(data), nil
}

func (m *defaultSystemModel) Update(ctx context.Context, data *System) error {
	updated, err := m.db.System.UpdateOneID(data.Id).
		SetCategory(data.Category).
		SetKey(data.Key).
		SetValue(data.Value).
		SetType(data.Type).
		SetDesc(data.Desc).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, updated)
	return nil
}

func (m *defaultSystemModel) Delete(ctx context.Context, id int64) error {
	err := m.db.System.DeleteOneID(id).Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

func systemFromEnt(data *ent.System) *System {
	if data == nil {
		return nil
	}
	var resp System
	copyFromEnt(&resp, data)
	return &resp
}

func copyFromEnt(dst *System, src *ent.System) {
	dst.Id = src.ID
	dst.Category = src.Category
	dst.Key = src.Key
	dst.Value = src.Value
	dst.Type = src.Type
	dst.Desc = src.Desc
	dst.CreatedAt = src.CreatedAt
	dst.UpdatedAt = src.UpdatedAt
}
