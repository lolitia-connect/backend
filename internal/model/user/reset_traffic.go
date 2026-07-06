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

func (m *customUserModel) ResetSubscribeTrafficByIds(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := m.db.UserSubscribe.Update().Where(entsub.IDIn(ids...)).SetUpload(0).SetDownload(0).SetStatus(1).ClearFinishedAt().Save(ctx)
	return err
}

func (m *customUserModel) queryResetSubscribeIds(ctx context.Context, subscribeIds []int64, now time.Time, mode string) ([]int64, error) {
	items, err := m.db.UserSubscribe.Query().Where(
		entsub.SubscribeIDIn(subscribeIds...),
		entsub.StatusIn(1, 2),
		entsub.StartTimeLTE(now),
		entsub.Or(entsub.ExpireTimeIsNil(), entsub.ExpireTime(time.UnixMilli(0)), entsub.ExpireTimeGT(now)),
	).Select(entsub.FieldID, entsub.FieldStartTime).All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		if mode == "monthly" && !matchesMonthlyReset(item.StartTime, now) {
			continue
		}
		if mode == "yearly" && !matchesYearlyReset(item.StartTime, now) {
			continue
		}
		ids = append(ids, item.ID)
	}
	return ids, nil
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
