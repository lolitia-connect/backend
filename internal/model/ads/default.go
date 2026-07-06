package ads

import (
	"context"

	"github.com/perfect-panel/server/ent"
	entads "github.com/perfect-panel/server/ent/ads"
)

var _ Model = (*customAdsModel)(nil)
var (
	cacheAdsIdPrefix = "cache:ads:id:"
)

type (
	Model interface {
		adsModel
		customAdsLogicModel
	}
	adsModel interface {
		Insert(ctx context.Context, data *Ads) error
		FindOne(ctx context.Context, id int64) (*Ads, error)
		Update(ctx context.Context, data *Ads) error
		Delete(ctx context.Context, id int64) error
	}

	customAdsModel struct {
		*defaultAdsModel
	}
	defaultAdsModel struct {
		db *ent.Client
	}
)

func newAdsModel(db *ent.Client) *defaultAdsModel {
	return &defaultAdsModel{
		db: db,
	}
}

func (m *defaultAdsModel) Insert(ctx context.Context, data *Ads) error {
	created, err := m.db.Ads.Create().
		SetTitle(data.Title).
		SetType(data.Type).
		SetContent(data.Content).
		SetDescription(data.Description).
		SetTargetURL(data.TargetURL).
		SetStartTime(data.StartTime).
		SetEndTime(data.EndTime).
		SetStatus(data.Status).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, created)
	return nil
}

func (m *defaultAdsModel) FindOne(ctx context.Context, id int64) (*Ads, error) {
	data, err := m.db.Ads.Query().Where(entads.ID(id)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return adsFromEnt(data), nil
}

func (m *defaultAdsModel) Update(ctx context.Context, data *Ads) error {
	updated, err := m.db.Ads.UpdateOneID(data.Id).
		SetTitle(data.Title).
		SetType(data.Type).
		SetContent(data.Content).
		SetDescription(data.Description).
		SetTargetURL(data.TargetURL).
		SetStartTime(data.StartTime).
		SetEndTime(data.EndTime).
		SetStatus(data.Status).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, updated)
	return nil
}

func (m *defaultAdsModel) Delete(ctx context.Context, id int64) error {
	err := m.db.Ads.DeleteOneID(id).Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

func adsFromEnt(data *ent.Ads) *Ads {
	if data == nil {
		return nil
	}
	var resp Ads
	copyFromEnt(&resp, data)
	return &resp
}

func copyFromEnt(dst *Ads, src *ent.Ads) {
	dst.Id = src.ID
	dst.Title = src.Title
	dst.Type = src.Type
	dst.Content = src.Content
	dst.Description = src.Description
	dst.TargetURL = src.TargetURL
	dst.StartTime = src.StartTime
	dst.EndTime = src.EndTime
	dst.Status = src.Status
	dst.CreatedAt = src.CreatedAt
	dst.UpdatedAt = src.UpdatedAt
}
