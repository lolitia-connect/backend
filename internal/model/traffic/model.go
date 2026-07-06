package traffic

import (
	"context"
	"fmt"
	"sort"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/predicate"
	enttrafficlog "github.com/perfect-panel/server/ent/trafficlog"
)

type customTrafficLogicModel interface {
	QueryServerTrafficByDay(ctx context.Context, serverId int64, date time.Time) (*TotalTraffic, error)
	QueryTrafficByDay(ctx context.Context, date time.Time) (*TotalTraffic, error)
	QueryTrafficByMonthly(ctx context.Context, date time.Time) (*TotalTraffic, error)
	QueryTrafficSummary(ctx context.Context, start, end time.Time) (*TotalTraffic, error)
	TopServersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]ServerTrafficRanking, error)
	TopServersTrafficByMonthly(ctx context.Context, date time.Time, limit int) ([]ServerTrafficRanking, error)
	TopUsersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]UserTrafficRanking, error)
	TopUsersTrafficByMonthly(ctx context.Context, date time.Time, limit int) ([]UserTrafficRanking, error)
	QueryServerTrafficRanking(ctx context.Context, start, end time.Time) ([]ServerTrafficRanking, error)
	QueryUserTrafficRanking(ctx context.Context, start, end time.Time) ([]UserTrafficRanking, error)
	QueryTrafficLogPageList(ctx context.Context, userId, subscribeId int64, page, size int) ([]*TrafficLog, int64, error)
	QueryTrafficLogDetails(ctx context.Context, filter *TrafficLogDetailsFilter) ([]*TrafficLog, int64, error)
	DeleteBefore(ctx context.Context, end time.Time) error
}

type TrafficLogDetailsFilter struct {
	ServerId    int64
	UserId      int64
	SubscribeId int64
	Start       time.Time
	End         time.Time
	Page        int
	Size        int
}

// NewModel returns a model for the database table.
func NewModel(conn *ent.Client) Model {
	return &customTrafficModel{
		defaultTrafficModel: newTrafficModel(conn),
	}
}

func (m *customTrafficModel) QueryServerTrafficByDay(ctx context.Context, serverId int64, date time.Time) (*TotalTraffic, error) {
	var data TotalTraffic
	start, end := dayRange(date)
	err := m.db.TrafficLog.Query().Where(enttrafficlog.ServerID(serverId), timeRange(start, end)).Aggregate(totalTrafficAggregates()...).Scan(ctx, &data)
	return &data, err
}

func (m *customTrafficModel) QueryTrafficByDay(ctx context.Context, date time.Time) (*TotalTraffic, error) {
	var data TotalTraffic
	start, end := dayRange(date)
	err := m.db.TrafficLog.Query().Where(timeRange(start, end)).Aggregate(totalTrafficAggregates()...).Scan(ctx, &data)
	return &data, err
}

func (m *customTrafficModel) QueryTrafficByMonthly(ctx context.Context, date time.Time) (*TotalTraffic, error) {
	var data TotalTraffic
	start, end := monthRange(date)
	err := m.db.TrafficLog.Query().Where(timeRange(start, end)).Aggregate(totalTrafficAggregates()...).Scan(ctx, &data)
	return &data, err
}

func (m *customTrafficModel) QueryTrafficSummary(ctx context.Context, start, end time.Time) (*TotalTraffic, error) {
	var data TotalTraffic
	err := m.db.TrafficLog.Query().Where(timeRange(start, end)).Aggregate(totalTrafficAggregates()...).Scan(ctx, &data)
	return &data, err
}

func (m *customTrafficModel) TopServersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]ServerTrafficRanking, error) {
	var summaries []ServerTrafficRanking
	start, end := dayRange(date)
	err := m.serverTrafficRankingQuery(ctx, start, end, limit, &summaries)
	return summaries, err
}

func (m *customTrafficModel) TopServersTrafficByMonthly(ctx context.Context, date time.Time, limit int) ([]ServerTrafficRanking, error) {
	var summaries []ServerTrafficRanking
	start, end := monthRange(date)
	err := m.serverTrafficRankingQuery(ctx, start, end, limit, &summaries)
	return summaries, err
}

func (m *customTrafficModel) TopUsersTrafficByDay(ctx context.Context, date time.Time, limit int) ([]UserTrafficRanking, error) {
	var summaries []UserTrafficRanking
	start, end := dayRange(date)
	err := m.userTrafficRankingQuery(ctx, start, end, limit, &summaries)
	return summaries, err
}

func (m *customTrafficModel) TopUsersTrafficByMonthly(ctx context.Context, date time.Time, limit int) ([]UserTrafficRanking, error) {
	var summaries []UserTrafficRanking
	start, end := monthRange(date)
	err := m.userTrafficRankingQuery(ctx, start, end, limit, &summaries)
	return summaries, err
}

func (m *customTrafficModel) QueryServerTrafficRanking(ctx context.Context, start, end time.Time) ([]ServerTrafficRanking, error) {
	var summaries []ServerTrafficRanking
	err := m.serverTrafficRankingQuery(ctx, start, end, 0, &summaries)
	return summaries, err
}

func (m *customTrafficModel) QueryUserTrafficRanking(ctx context.Context, start, end time.Time) ([]UserTrafficRanking, error) {
	var summaries []UserTrafficRanking
	err := m.userTrafficRankingQuery(ctx, start, end, 0, &summaries)
	return summaries, err
}

// QueryTrafficLogPageList returns a list of records that meet the conditions.
func (m *customTrafficModel) QueryTrafficLogPageList(ctx context.Context, userId, subscribeId int64, page, size int) ([]*TrafficLog, int64, error) {
	query := m.db.TrafficLog.Query().Where(enttrafficlog.UserID(userId), enttrafficlog.SubscribeID(subscribeId))
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	data, err := query.Limit(size).Offset((page - 1) * size).All(ctx)
	return entTrafficLogsToModel(data), int64(total), err
}

func (m *customTrafficModel) QueryTrafficLogDetails(ctx context.Context, filter *TrafficLogDetailsFilter) ([]*TrafficLog, int64, error) {
	if filter == nil {
		filter = &TrafficLogDetailsFilter{Page: 1, Size: 10}
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Size < 1 {
		filter.Size = 10
	}

	predicates := make([]predicate.TrafficLog, 0)
	if filter.ServerId != 0 {
		predicates = append(predicates, enttrafficlog.ServerID(filter.ServerId))
	}
	if !filter.Start.IsZero() && !filter.End.IsZero() {
		predicates = append(predicates, timeRange(filter.Start, filter.End))
	}
	if filter.UserId != 0 {
		predicates = append(predicates, enttrafficlog.UserID(filter.UserId))
	}
	if filter.SubscribeId != 0 {
		predicates = append(predicates, enttrafficlog.SubscribeID(filter.SubscribeId))
	}

	query := m.db.TrafficLog.Query().Where(predicates...)
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	data, err := query.Order(enttrafficlog.ByTimestamp(sql.OrderDesc())).Limit(filter.Size).Offset((filter.Page - 1) * filter.Size).All(ctx)
	return entTrafficLogsToModel(data), int64(total), err
}

func (m *customTrafficModel) DeleteBefore(ctx context.Context, end time.Time) error {
	_, err := m.db.TrafficLog.Delete().Where(enttrafficlog.TimestampLTE(end)).Exec(ctx)
	return err
}

func dayRange(date time.Time) (time.Time, time.Time) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	return start, start.Add(24 * time.Hour)
}

func monthRange(date time.Time) (time.Time, time.Time) {
	start := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	return start, start.AddDate(0, 1, 0)
}

func (m *customTrafficModel) serverTrafficRankingQuery(ctx context.Context, start, end time.Time, limit int, dst *[]ServerTrafficRanking) error {
	err := m.db.TrafficLog.Query().Where(timeRange(start, end)).GroupBy(enttrafficlog.FieldServerID).Aggregate(trafficRankingAggregates()...).Scan(ctx, dst)
	if err != nil {
		return err
	}
	sort.Slice(*dst, func(i, j int) bool { return (*dst)[i].Total > (*dst)[j].Total })
	if limit > 0 && len(*dst) > limit {
		*dst = (*dst)[:limit]
	}
	return nil
}

func (m *customTrafficModel) userTrafficRankingQuery(ctx context.Context, start, end time.Time, limit int, dst *[]UserTrafficRanking) error {
	err := m.db.TrafficLog.Query().Where(timeRange(start, end)).GroupBy(enttrafficlog.FieldUserID, enttrafficlog.FieldSubscribeID).Aggregate(trafficRankingAggregates()...).Scan(ctx, dst)
	if err != nil {
		return err
	}
	sort.Slice(*dst, func(i, j int) bool { return (*dst)[i].Total > (*dst)[j].Total })
	if limit > 0 && len(*dst) > limit {
		*dst = (*dst)[:limit]
	}
	return nil
}

func timeRange(start, end time.Time) predicate.TrafficLog {
	return predicate.TrafficLog(func(s *sql.Selector) {
		s.Where(sql.And(sql.GTE(s.C(enttrafficlog.FieldTimestamp), start), sql.LT(s.C(enttrafficlog.FieldTimestamp), end)))
	})
}

func totalTrafficAggregates() []ent.AggregateFunc {
	return []ent.AggregateFunc{
		ent.As(sumField(enttrafficlog.FieldDownload), "download"),
		ent.As(sumField(enttrafficlog.FieldUpload), "upload"),
	}
}

func trafficRankingAggregates() []ent.AggregateFunc {
	return []ent.AggregateFunc{
		ent.As(sumTotal(), "total"),
		ent.As(sumField(enttrafficlog.FieldDownload), "download"),
		ent.As(sumField(enttrafficlog.FieldUpload), "upload"),
	}
}

func sumField(field string) ent.AggregateFunc {
	return func(s *sql.Selector) string {
		return fmt.Sprintf("COALESCE(SUM(%s), 0)", s.C(field))
	}
}

func sumTotal() ent.AggregateFunc {
	return func(s *sql.Selector) string {
		return fmt.Sprintf("COALESCE(SUM(%s + %s), 0)", s.C(enttrafficlog.FieldDownload), s.C(enttrafficlog.FieldUpload))
	}
}

func trafficColumn(column string) string {
	return sql.Table(enttrafficlog.Table).C(column)
}

func entTrafficLogsToModel(data []*ent.TrafficLog) []*TrafficLog {
	result := make([]*TrafficLog, 0, len(data))
	for _, item := range data {
		result = append(result, entToTrafficLog(item))
	}
	return result
}
