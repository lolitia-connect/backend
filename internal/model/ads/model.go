package ads

import (
	"context"

	"github.com/perfect-panel/server/ent"
	entads "github.com/perfect-panel/server/ent/ads"
)

type customAdsLogicModel interface {
	GetAdsListByPage(ctx context.Context, page, size int, filter Filter) (int64, []*Ads, error)
}

// NewModel returns a model for the database table.
func NewModel(conn *ent.Client) Model {
	return &customAdsModel{
		defaultAdsModel: newAdsModel(conn),
	}
}

type Filter struct {
	Status *int
	Search string
}

// GetAdsListByPage  get ads list by page
func (m *customAdsModel) GetAdsListByPage(ctx context.Context, page, size int, filter Filter) (int64, []*Ads, error) {
	query := m.db.Ads.Query()
	if filter.Status != nil {
		query = query.Where(entads.Status(*filter.Status))
	}
	if filter.Search != "" {
		query = query.Where(entads.Or(entads.TitleContains(filter.Search), entads.ContentContains(filter.Search)))
	}
	total, err := query.Count(ctx)
	if err != nil {
		return 0, nil, err
	}
	items, err := query.Offset((page - 1) * size).Limit(size).All(ctx)
	if err != nil {
		return 0, nil, err
	}
	list := make([]*Ads, 0, len(items))
	for _, item := range items {
		list = append(list, adsFromEnt(item))
	}
	return int64(total), list, nil
}
