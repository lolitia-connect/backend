package announcement

import (
	"context"

	"github.com/perfect-panel/server/ent"
	entannouncement "github.com/perfect-panel/server/ent/announcement"
)

type customAnnouncementLogicModel interface {
	GetAnnouncementListByPage(ctx context.Context, page, size int, filter Filter) (int64, []*Announcement, error)
}

// NewModel returns a model for the database table.
func NewModel(conn *ent.Client) Model {
	return &customAnnouncementModel{
		defaultAnnouncementModel: newAnnouncementModel(conn),
	}
}

type Filter struct {
	Show   *bool
	Pinned *bool
	Popup  *bool
	Search string
}

// GetAnnouncementListByPage  get announcement list by page
func (m *customAnnouncementModel) GetAnnouncementListByPage(ctx context.Context, page, size int, filter Filter) (int64, []*Announcement, error) {
	if size == 0 {
		size = 10
	}
	query := m.db.Announcement.Query()
	if filter.Show != nil {
		query = query.Where(entannouncement.Show(*filter.Show))
	}
	if filter.Pinned != nil {
		query = query.Where(entannouncement.Pinned(*filter.Pinned))
	}
	if filter.Popup != nil {
		query = query.Where(entannouncement.Popup(*filter.Popup))
	}
	if filter.Search != "" {
		query = query.Where(entannouncement.Or(entannouncement.TitleContains(filter.Search), entannouncement.ContentContains(filter.Search)))
	}
	total, err := query.Count(ctx)
	if err != nil {
		return 0, nil, err
	}
	items, err := query.Offset((page - 1) * size).Limit(size).All(ctx)
	if err != nil {
		return 0, nil, err
	}
	list := make([]*Announcement, 0, len(items))
	for _, item := range items {
		list = append(list, announcementFromEnt(item))
	}
	return int64(total), list, nil
}
