package log

import (
	"context"

	"github.com/perfect-panel/server/ent"
	entsystemlog "github.com/perfect-panel/server/ent/systemlog"
)

func NewModel(db *ent.Client) Model {
	return &customSystemLogModel{
		defaultLogModel: newSystemLogModel(db),
	}
}

type FilterParams struct {
	Page     int
	Size     int
	Type     uint8
	Data     string
	Search   string
	ObjectID int64
}

type customSystemLogLogicModel interface {
	FilterSystemLog(ctx context.Context, filter *FilterParams) ([]*SystemLog, int64, error)
	FindFirstByDateType(ctx context.Context, date string, typ uint8) (*SystemLog, error)
	FindByDatesType(ctx context.Context, dates []string, typ uint8) ([]*SystemLog, error)
}

func (m *customSystemLogModel) FilterSystemLog(ctx context.Context, filter *FilterParams) ([]*SystemLog, int64, error) {
	if filter == nil {
		filter = &FilterParams{
			Page: 1,
			Size: 10,
		}
	}

	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Size < 1 {
		filter.Size = 10
	}

	query := m.db.SystemLog.Query()
	if filter.Type != 0 {
		query = query.Where(entsystemlog.TypeEQ(filter.Type))
	}

	if filter.Data != "" {
		query = query.Where(entsystemlog.DateEQ(filter.Data))
	}

	if filter.ObjectID != 0 {
		query = query.Where(entsystemlog.ObjectIDEQ(filter.ObjectID))
	}
	if filter.Search != "" {
		query = query.Where(entsystemlog.ContentContains(filter.Search))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	items, err := query.Order(ent.Desc(entsystemlog.FieldID)).Limit(filter.Size).Offset((filter.Page - 1) * filter.Size).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	logs := make([]*SystemLog, 0, len(items))
	for _, item := range items {
		logs = append(logs, logFromEnt(item))
	}
	return logs, int64(total), nil
}

func (m *customSystemLogModel) FindFirstByDateType(ctx context.Context, date string, typ uint8) (*SystemLog, error) {
	data, err := m.db.SystemLog.Query().Where(entsystemlog.DateEQ(date), entsystemlog.TypeEQ(typ)).First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return logFromEnt(data), nil
}

func (m *customSystemLogModel) FindByDatesType(ctx context.Context, dates []string, typ uint8) ([]*SystemLog, error) {
	if len(dates) == 0 {
		return []*SystemLog{}, nil
	}
	items, err := m.db.SystemLog.Query().Where(entsystemlog.DateIn(dates...), entsystemlog.TypeEQ(typ)).All(ctx)
	if err != nil {
		return nil, err
	}
	data := make([]*SystemLog, 0, len(items))
	for _, item := range items {
		data = append(data, logFromEnt(item))
	}
	return data, nil
}
