package user

import (
	"context"
	"time"

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

func (m *customUserModel) ResetSubscribeTrafficByIds(ctx context.Context, ids []int64, now time.Time) ([]*Subscribe, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	changed := make([]int64, 0, len(ids))
	for _, id := range ids {
		count, err := m.db.UserSubscribe.Update().Where(entsub.ID(id), entsub.StatusIn(1, 2), entsub.EntitlementSourceEQ(""), entsub.StartTimeLTE(now),
			entsub.Or(entsub.TrafficResetAtIsNil(), entsub.TrafficResetAtLT(day)),
			entsub.Or(entsub.ExpireTimeIsNil(), entsub.ExpireTimeLT(time.Date(1971, 1, 1, 0, 0, 0, 0, time.UTC)), entsub.ExpireTimeGT(now)),
		).SetUpload(0).SetDownload(0).SetStatus(1).ClearFinishedAt().SetTrafficResetAt(day).Save(ctx)
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

func (m *customUserModel) queryResetSubscribeIds(ctx context.Context, subscribeIds []int64, now time.Time, mode string) ([]int64, error) {
	items, err := m.db.UserSubscribe.Query().Where(
		entsub.SubscribeIDIn(subscribeIds...),
		entsub.StatusIn(1, 2),
		entsub.EntitlementSourceEQ(""),
		entsub.StartTimeLTE(now),
		entsub.Or(entsub.ExpireTimeIsNil(), entsub.ExpireTimeLT(time.Date(1971, 1, 1, 0, 0, 0, 0, time.UTC)), entsub.ExpireTimeGT(now)),
	).Select(entsub.FieldID, entsub.FieldStartTime, entsub.FieldTrafficResetAt).All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		if resetOwed(item.StartTime, item.TrafficResetAt, now, mode) {
			ids = append(ids, item.ID)
		}
	}
	return ids, nil
}

func resetOwed(start time.Time, marker *time.Time, now time.Time, mode string) bool {
	start = start.In(now.Location())
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	resetDate := func(year int, month time.Month, resetDay int) time.Time {
		first := time.Date(year, month, 1, 0, 0, 0, 0, now.Location())
		last := first.AddDate(0, 1, -1).Day()
		return time.Date(first.Year(), first.Month(), min(resetDay, last), 0, 0, 0, 0, now.Location())
	}
	var due time.Time
	switch mode {
	case "first":
		due = resetDate(day.Year(), day.Month(), 1)
	case "monthly":
		due = resetDate(day.Year(), day.Month(), start.Day())
		if due.After(day) {
			due = resetDate(day.Year(), day.Month()-1, start.Day())
		}
	case "yearly":
		due = resetDate(day.Year(), start.Month(), start.Day())
		if due.After(day) {
			due = resetDate(day.Year()-1, start.Month(), start.Day())
		}
	default:
		return false
	}
	if due.Before(time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, now.Location())) {
		return false
	}
	if marker == nil {
		return due.Equal(day)
	}
	marked := marker.In(now.Location())
	markedDay := time.Date(marked.Year(), marked.Month(), marked.Day(), 0, 0, 0, 0, now.Location())
	return due.After(markedDay)
}

func matchesMonthlyReset(start, now time.Time) bool {
	if isLastDayOfMonth(now) {
		return start.Day() >= now.Day()
	}
	return start.Day() == now.Day()
}

func matchesYearlyReset(start, now time.Time) bool {
	if now.Month() == time.February && now.Day() == 28 && !isLeapYear(now.Year()) {
		return start.Month() == time.February && (start.Day() == 28 || start.Day() == 29)
	}
	return start.Month() == now.Month() && start.Day() == now.Day()
}

func isLastDayOfMonth(t time.Time) bool {
	return t.AddDate(0, 0, 1).Month() != t.Month()
}

func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}
