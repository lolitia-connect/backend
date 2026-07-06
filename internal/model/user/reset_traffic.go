package user

import (
	"context"
	"fmt"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent/predicate"
	entsub "github.com/perfect-panel/server/ent/usersubscribe"
)

func (m *customUserModel) QueryMonthlyResetSubscribeIds(ctx context.Context, subscribeIds []int64, now time.Time) ([]int64, error) {
	var ids []int64
	if len(subscribeIds) == 0 {
		return ids, nil
	}
	return m.queryResetSubscribeIds(ctx, subscribeIds, now, "monthly")
}

func (m *customUserModel) QueryFirstResetSubscribeIds(ctx context.Context, subscribeIds []int64, now time.Time) ([]int64, error) {
	var ids []int64
	if len(subscribeIds) == 0 {
		return ids, nil
	}
	return m.queryResetSubscribeIds(ctx, subscribeIds, now, "first")
}

func (m *customUserModel) QueryYearlyResetSubscribeIds(ctx context.Context, subscribeIds []int64, now time.Time) ([]int64, error) {
	var ids []int64
	if len(subscribeIds) == 0 {
		return ids, nil
	}
	return m.queryResetSubscribeIds(ctx, subscribeIds, now, "yearly")
}

func (m *customUserModel) ResetSubscribeTrafficByIds(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := m.db.UserSubscribe.Update().Where(entsub.IDIn(ids...)).SetUpload(0).SetDownload(0).SetStatus(1).ClearFinishedAt().Save(ctx)
	return err
}

func extractColumnDatePart(clientDialect, column, part string) string {
	if clientDialect == "postgres" {
		return fmt.Sprintf("EXTRACT(%s FROM %s)", part, column)
	}
	switch part {
	case "month":
		return fmt.Sprintf("MONTH(%s)", column)
	default:
		return fmt.Sprintf("DAY(%s)", column)
	}
}

func (m *customUserModel) queryResetSubscribeIds(ctx context.Context, subscribeIds []int64, now time.Time, mode string) ([]int64, error) {
	var ids []int64
	query := m.db.UserSubscribe.Query().Where(
		entsub.SubscribeIDIn(subscribeIds...),
		entsub.StatusIn(1, 2),
		entsub.StartTimeLTE(now),
		predicate.UserSubscribe(func(s *entsql.Selector) {
			s.Where(entsql.Or(entsql.IsNull(s.C(entsub.FieldExpireTime)), entsql.EQ(s.C(entsub.FieldExpireTime), time.UnixMilli(0)), entsql.GT(s.C(entsub.FieldExpireTime), now)))
		}),
	)
	if mode == "monthly" {
		query = query.Where(monthlyResetDatePredicate(m.db.Driver().Dialect(), now))
	} else if mode == "yearly" {
		query = query.Where(yearlyResetDatePredicate(m.db.Driver().Dialect(), now))
	}
	err := query.Select(entsub.FieldID).Scan(ctx, &ids)
	return ids, err
}

func monthlyResetDatePredicate(dialect string, now time.Time) predicate.UserSubscribe {
	return predicate.UserSubscribe(func(s *entsql.Selector) {
		dayExpr := extractColumnDatePart(dialect, s.C(entsub.FieldStartTime), "day")
		if isLastDayOfMonth(now) {
			s.Where(entsql.P(func(b *entsql.Builder) { b.WriteString(dayExpr).WriteString(" >= ").Arg(now.Day()) }))
			return
		}
		s.Where(entsql.P(func(b *entsql.Builder) { b.WriteString(dayExpr).WriteString(" = ").Arg(now.Day()) }))
	})
}

func yearlyResetDatePredicate(dialect string, now time.Time) predicate.UserSubscribe {
	return predicate.UserSubscribe(func(s *entsql.Selector) {
		monthExpr := extractColumnDatePart(dialect, s.C(entsub.FieldStartTime), "month")
		dayExpr := extractColumnDatePart(dialect, s.C(entsub.FieldStartTime), "day")
		if now.Month() == time.February && now.Day() == 28 && !isLeapYear(now.Year()) {
			s.Where(entsql.P(func(b *entsql.Builder) {
				b.WriteString(monthExpr).WriteString(" = ").Arg(int(time.February)).WriteString(" AND ").WriteString(dayExpr).WriteString(" IN (").Arg(28).WriteString(", ").Arg(29).WriteString(")")
			}))
			return
		}
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.WriteString(monthExpr).WriteString(" = ").Arg(int(now.Month())).WriteString(" AND ").WriteString(dayExpr).WriteString(" = ").Arg(now.Day())
		}))
	})
}

func isLastDayOfMonth(t time.Time) bool {
	return t.AddDate(0, 0, 1).Month() != t.Month()
}

func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}
