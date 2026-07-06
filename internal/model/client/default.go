package client

import (
	"context"
	"time"

	"github.com/perfect-panel/server/ent"
)

type (
	Model interface {
		subscribeApplicationModel
	}
	subscribeApplicationModel interface {
		Insert(ctx context.Context, data *SubscribeApplication) error
		FindOne(ctx context.Context, id int64) (*SubscribeApplication, error)
		Update(ctx context.Context, data *SubscribeApplication) error
		Delete(ctx context.Context, id int64) error
		List(ctx context.Context) ([]*SubscribeApplication, error)
	}
	DefaultSubscribeApplicationModel struct {
		db *ent.Client
	}
)

func NewSubscribeApplicationModel(db *ent.Client) Model {
	return &DefaultSubscribeApplicationModel{
		db: db,
	}
}

func (m *DefaultSubscribeApplicationModel) Insert(ctx context.Context, data *SubscribeApplication) error {
	created, err := m.db.SubscribeApplication.Create().
		SetName(data.Name).
		SetIcon(data.Icon).
		SetDescription(data.Description).
		SetScheme(data.Scheme).
		SetUserAgent(data.UserAgent).
		SetIsDefault(data.IsDefault).
		SetSubscribeTemplate(data.SubscribeTemplate).
		SetOutputFormat(data.OutputFormat).
		SetDownloadLink(data.DownloadLink).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, created)
	return nil
}

func (m *DefaultSubscribeApplicationModel) FindOne(ctx context.Context, id int64) (*SubscribeApplication, error) {
	resp, err := m.db.SubscribeApplication.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return subscribeApplicationFromEnt(resp), nil
}

func (m *DefaultSubscribeApplicationModel) Update(ctx context.Context, data *SubscribeApplication) error {
	if _, err := m.FindOne(ctx, data.Id); err != nil {
		return err
	}
	updated, err := m.db.SubscribeApplication.UpdateOneID(data.Id).
		SetName(data.Name).
		SetIcon(data.Icon).
		SetDescription(data.Description).
		SetScheme(data.Scheme).
		SetUserAgent(data.UserAgent).
		SetIsDefault(data.IsDefault).
		SetSubscribeTemplate(data.SubscribeTemplate).
		SetOutputFormat(data.OutputFormat).
		SetDownloadLink(data.DownloadLink).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, updated)
	return nil
}

func (m *DefaultSubscribeApplicationModel) Delete(ctx context.Context, id int64) error {
	return m.db.SubscribeApplication.DeleteOneID(id).Exec(ctx)
}

func (m *DefaultSubscribeApplicationModel) List(ctx context.Context) ([]*SubscribeApplication, error) {
	items, err := m.db.SubscribeApplication.Query().All(ctx)
	if err != nil {
		return nil, err
	}
	resp := make([]*SubscribeApplication, 0, len(items))
	for _, item := range items {
		resp = append(resp, subscribeApplicationFromEnt(item))
	}
	return resp, nil
}

func subscribeApplicationFromEnt(data *ent.SubscribeApplication) *SubscribeApplication {
	if data == nil {
		return nil
	}
	return &SubscribeApplication{
		Id:                data.ID,
		Name:              data.Name,
		Icon:              data.Icon,
		Description:       data.Description,
		Scheme:            data.Scheme,
		UserAgent:         data.UserAgent,
		IsDefault:         data.IsDefault,
		SubscribeTemplate: data.SubscribeTemplate,
		OutputFormat:      data.OutputFormat,
		DownloadLink:      data.DownloadLink,
		CreatedAt:         data.CreatedAt,
		UpdatedAt:         data.UpdatedAt,
	}
}

func copyFromEnt(dst *SubscribeApplication, src *ent.SubscribeApplication) {
	if dst == nil || src == nil {
		return
	}
	*dst = *subscribeApplicationFromEnt(src)
}
