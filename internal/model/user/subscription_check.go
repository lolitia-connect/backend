package user

import (
	"context"
	"fmt"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent/predicate"
	entsub "github.com/perfect-panel/server/ent/usersubscribe"
)

func (m *customUserModel) FindTrafficExceededSubscribes(ctx context.Context) ([]*Subscribe, error) {
	items, err := m.db.UserSubscribe.Query().Where(entsub.StatusIn(0, 1), entsub.EntitlementSourceEQ(""), entsub.TrafficGT(0), entsub.TrafficUnlimited(false), predicate.UserSubscribe(func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.Ident(s.C(entsub.FieldUpload)).WriteString(" + ").Ident(s.C(entsub.FieldDownload)).WriteString(" >= ").Ident(s.C(entsub.FieldTraffic))
		}))
	})).All(ctx)
	return entSubscribesToModels(items), err
}

func (m *customUserModel) FindExpiredSubscribes(ctx context.Context, now time.Time) ([]*Subscribe, error) {
	items, err := m.db.UserSubscribe.Query().Where(entsub.StatusIn(0, 1), entsub.EntitlementSourceEQ(""), entsub.ExpireTimeLT(now), entsub.ExpireTimeGTE(time.Date(1971, 1, 1, 0, 0, 0, 0, time.UTC)), entsub.FinishedAtIsNil()).All(ctx)
	return entSubscribesToModels(items), err
}

func finishConditions(status uint8, now time.Time) ([]predicate.UserSubscribe, error) {
	conditions := []predicate.UserSubscribe{entsub.StatusIn(0, 1), entsub.EntitlementSourceEQ("")}
	switch status {
	case 2:
		conditions = append(conditions, entsub.TrafficGT(0), entsub.TrafficUnlimited(false), func(selector *entsql.Selector) {
			selector.Where(entsql.P(func(builder *entsql.Builder) {
				builder.Ident(selector.C(entsub.FieldUpload)).WriteString(" + ").Ident(selector.C(entsub.FieldDownload)).WriteString(" >= ").Ident(selector.C(entsub.FieldTraffic))
			}))
		})
	case 3:
		conditions = append(conditions, entsub.ExpireTimeLT(now), entsub.ExpireTimeGTE(time.Date(1971, 1, 1, 0, 0, 0, 0, time.UTC)), entsub.FinishedAtIsNil())
	default:
		return nil, fmt.Errorf("unsupported subscription finish status: %d", status)
	}
	return conditions, nil
}

func (m *customUserModel) MarkSubscribesFinished(ctx context.Context, ids []int64, status uint8, finishedAt time.Time) ([]*Subscribe, error) {
	conditions, err := finishConditions(status, finishedAt)
	if err != nil {
		return nil, err
	}
	changed := make([]int64, 0, len(ids))
	for _, id := range ids {
		count, err := m.db.UserSubscribe.Update().Where(conditions...).Where(entsub.ID(id)).SetStatus(status).SetFinishedAt(finishedAt).Save(ctx)
		if err != nil {
			return nil, err
		}
		if count == 1 {
			changed = append(changed, id)
		}
	}
	if len(changed) == 0 {
		return nil, nil
	}
	return m.FindSubscribesByIds(ctx, changed)
}
