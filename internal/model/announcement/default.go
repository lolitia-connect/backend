package announcement

import (
	"context"

	"github.com/perfect-panel/server/ent"
	entannouncement "github.com/perfect-panel/server/ent/announcement"
)

var _ Model = (*customAnnouncementModel)(nil)
var (
	cacheAnnouncementIdPrefix = "cache:announcement:id:"
)

type (
	Model interface {
		announcementModel
		customAnnouncementLogicModel
	}
	announcementModel interface {
		Insert(ctx context.Context, data *Announcement) error
		FindOne(ctx context.Context, id int64) (*Announcement, error)
		Update(ctx context.Context, data *Announcement) error
		Delete(ctx context.Context, id int64) error
	}

	customAnnouncementModel struct {
		*defaultAnnouncementModel
	}
	defaultAnnouncementModel struct {
		db *ent.Client
	}
)

func newAnnouncementModel(db *ent.Client) *defaultAnnouncementModel {
	return &defaultAnnouncementModel{
		db: db,
	}
}

func (m *defaultAnnouncementModel) Insert(ctx context.Context, data *Announcement) error {
	created, err := m.db.Announcement.Create().
		SetTitle(data.Title).
		SetContent(data.Content).
		SetShow(value(data.Show)).
		SetPinned(value(data.Pinned)).
		SetPopup(value(data.Popup)).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, created)
	return nil
}

func (m *defaultAnnouncementModel) FindOne(ctx context.Context, id int64) (*Announcement, error) {
	data, err := m.db.Announcement.Query().Where(entannouncement.ID(id)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return announcementFromEnt(data), nil
}

func (m *defaultAnnouncementModel) Update(ctx context.Context, data *Announcement) error {
	updated, err := m.db.Announcement.UpdateOneID(data.Id).
		SetTitle(data.Title).
		SetContent(data.Content).
		SetShow(value(data.Show)).
		SetPinned(value(data.Pinned)).
		SetPopup(value(data.Popup)).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, updated)
	return nil
}

func (m *defaultAnnouncementModel) Delete(ctx context.Context, id int64) error {
	err := m.db.Announcement.DeleteOneID(id).Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

func announcementFromEnt(data *ent.Announcement) *Announcement {
	if data == nil {
		return nil
	}
	var resp Announcement
	copyFromEnt(&resp, data)
	return &resp
}

func copyFromEnt(dst *Announcement, src *ent.Announcement) {
	dst.Id = src.ID
	dst.Title = src.Title
	dst.Content = src.Content
	dst.Show = ptr(src.Show)
	dst.Pinned = ptr(src.Pinned)
	dst.Popup = ptr(src.Popup)
	dst.CreatedAt = src.CreatedAt
	dst.UpdatedAt = src.UpdatedAt
}

func ptr(v bool) *bool { return &v }

func value(v *bool) bool {
	return v != nil && *v
}
