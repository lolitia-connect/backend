package user

import (
	"context"
	"time"

	"github.com/perfect-panel/server/ent"
	entsub "github.com/perfect-panel/server/ent/usersubscribe"
)

func applySubscribeFilter(query *ent.UserSubscribeQuery, filter *SubscribeFilter) *ent.UserSubscribeQuery {
	if filter == nil {
		return query
	}
	if len(filter.Subscribers) > 0 {
		query = query.Where(entsub.SubscribeIDIn(filter.Subscribers...))
	}
	if filter.IsActive != nil && *filter.IsActive {
		query = query.Where(entsub.StatusIn(0, 1, 2))
	}
	if filter.StartTime != 0 {
		query = query.Where(entsub.StartTimeLTE(time.UnixMilli(filter.StartTime)))
	}
	if filter.EndTime != 0 {
		query = query.Where(entsub.ExpireTimeGTE(time.UnixMilli(filter.EndTime)))
	}
	return query
}

func (m *customUserModel) QuerySubscribeIdsByFilter(ctx context.Context, filter *SubscribeFilter) ([]int64, error) {
	var ids []int64
	err := applySubscribeFilter(m.db.UserSubscribe.Query(), filter).Select(entsub.FieldID).Scan(ctx, &ids)
	return ids, err
}

func (m *customUserModel) CountSubscribesByFilter(ctx context.Context, filter *SubscribeFilter) (int64, error) {
	count, err := applySubscribeFilter(m.db.UserSubscribe.Query(), filter).Count(ctx)
	return int64(count), err
}
