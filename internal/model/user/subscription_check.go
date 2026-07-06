package user

import (
	"context"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent/predicate"
	entsub "github.com/perfect-panel/server/ent/usersubscribe"
)

func (m *customUserModel) FindTrafficExceededSubscribes(ctx context.Context) ([]*Subscribe, error) {
	items, err := m.db.UserSubscribe.Query().Where(entsub.StatusIn(0, 1), entsub.TrafficGT(0), entsub.TrafficUnlimited(false), predicate.UserSubscribe(func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.Ident(s.C(entsub.FieldUpload)).WriteString(" + ").Ident(s.C(entsub.FieldDownload)).WriteString(" >= ").Ident(s.C(entsub.FieldTraffic))
		}))
	})).All(ctx)
	return entSubscribesToModels(items), err
}

func (m *customUserModel) FindExpiredSubscribes(ctx context.Context, now time.Time) ([]*Subscribe, error) {
	items, err := m.db.UserSubscribe.Query().Where(entsub.StatusIn(0, 1), entsub.ExpireTimeLT(now), entsub.ExpireTimeNEQ(time.UnixMilli(0)), entsub.FinishedAtIsNil()).All(ctx)
	return entSubscribesToModels(items), err
}

func (m *customUserModel) MarkSubscribesFinished(ctx context.Context, ids []int64, status uint8, finishedAt time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := m.db.UserSubscribe.Update().Where(entsub.IDIn(ids...)).SetStatus(status).SetFinishedAt(finishedAt).Save(ctx)
	return err
}
