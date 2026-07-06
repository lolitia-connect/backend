package user

import (
	"context"
	"sort"
	"time"

	entorder "github.com/perfect-panel/server/ent/order"
	entuser "github.com/perfect-panel/server/ent/user"
)

func (m *customUserModel) QueryResisterUserTotalByDate(ctx context.Context, date time.Time) (int64, error) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	end := start.AddDate(0, 0, 1)
	count, err := m.db.User.Query().Where(entuser.CreatedAtGTE(start), entuser.CreatedAtLT(end), entuser.DeletedAtIsNil()).Count(ctx)
	return int64(count), err
}

func (m *customUserModel) QueryResisterUserTotalByMonthly(ctx context.Context, date time.Time) (int64, error) {
	start := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	end := start.AddDate(0, 1, 0)
	count, err := m.db.User.Query().Where(entuser.CreatedAtGTE(start), entuser.CreatedAtLT(end), entuser.DeletedAtIsNil()).Count(ctx)
	return int64(count), err
}

func (m *customUserModel) QueryResisterUserTotal(ctx context.Context) (int64, error) {
	count, err := m.db.User.Query().Where(entuser.DeletedAtIsNil()).Count(ctx)
	return int64(count), err
}

func (m *customUserModel) CountEnabledUsers(ctx context.Context) (int64, error) {
	count, err := m.db.User.Query().Where(entuser.Enable(true), entuser.DeletedAtIsNil()).Count(ctx)
	return int64(count), err
}

// QueryDailyUserStatisticsList Query daily user statistics list for the current month (from 1st to current date)
func (m *customUserModel) QueryDailyUserStatisticsList(ctx context.Context, date time.Time) ([]UserStatisticsWithDate, error) {
	firstDay := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	return m.queryUserStatistics(ctx, firstDay, date, "day")
}

// QueryMonthlyUserStatisticsList Query monthly user statistics list for the past 6 months
func (m *customUserModel) QueryMonthlyUserStatisticsList(ctx context.Context, date time.Time) ([]UserStatisticsWithDate, error) {
	sixMonthsAgo := date.AddDate(0, -5, 0)
	return m.queryUserStatistics(ctx, sixMonthsAgo, date, "month")
}

func (m *customUserModel) queryUserStatistics(ctx context.Context, start, end time.Time, bucket string) ([]UserStatisticsWithDate, error) {
	users, err := m.db.User.Query().Where(entuser.CreatedAtGTE(start), entuser.CreatedAtLTE(end), entuser.DeletedAtIsNil()).Select(entuser.FieldCreatedAt).All(ctx)
	if err != nil {
		return nil, err
	}
	items := make(map[string]*UserStatisticsWithDate)
	for _, item := range users {
		key := formatUserBucket(item.CreatedAt, bucket)
		stat := items[key]
		if stat == nil {
			stat = &UserStatisticsWithDate{Date: key}
			items[key] = stat
		}
		stat.Register++
	}
	orders, err := m.db.Order.Query().Where(entorder.CreatedAtGTE(start), entorder.CreatedAtLTE(end), entorder.StatusIn(2, 5)).Select(entorder.FieldCreatedAt, entorder.FieldUserID, entorder.FieldIsNew).All(ctx)
	if err != nil {
		return nil, err
	}
	newUsers := make(map[string]map[int64]struct{})
	renewalUsers := make(map[string]map[int64]struct{})
	for _, item := range orders {
		key := formatUserBucket(item.CreatedAt, bucket)
		if _, ok := items[key]; !ok {
			continue
		}
		bucketUsers := renewalUsers[key]
		if item.IsNew {
			bucketUsers = newUsers[key]
		}
		if bucketUsers == nil {
			bucketUsers = make(map[int64]struct{})
			if item.IsNew {
				newUsers[key] = bucketUsers
			} else {
				renewalUsers[key] = bucketUsers
			}
		}
		bucketUsers[item.UserID] = struct{}{}
	}
	results := make([]UserStatisticsWithDate, 0, len(items))
	for key, item := range items {
		item.NewOrderUsers = int64(len(newUsers[key]))
		item.RenewalOrderUsers = int64(len(renewalUsers[key]))
		results = append(results, *item)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Date < results[j].Date })
	return results, nil
}

func formatUserBucket(date time.Time, bucket string) string {
	if bucket == "month" {
		return date.Format("2006-01")
	}
	return date.Format("2006-01-02")
}
