package log

import (
	"context"
	"encoding/json"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	"github.com/perfect-panel/server/ent"
	entsystemlog "github.com/perfect-panel/server/ent/systemlog"
)

func NewModel(db *ent.Client) Model {
	return &customSystemLogModel{
		defaultLogModel: newSystemLogModel(db),
	}
}

type FilterParams struct {
	Page         int
	Size         int
	Type         uint8
	Data         string
	StartDate    string
	EndDate      string
	Search       string
	ObjectID     int64
	ContentInt64 map[string]int64
	Offset       *int
}

// DateRangeMode describes which date filter is in effect for a log query.
type DateRangeMode uint8

const (
	// DateRangeNone disables date filtering.
	DateRangeNone DateRangeMode = iota
	// DateRangeExact matches a single day.
	DateRangeExact
	// DateRangeBetween matches an inclusive [start, end] window.
	DateRangeBetween
	// DateRangeFrom matches every day on or after start.
	DateRangeFrom
	// DateRangeTo matches every day on or before end.
	DateRangeTo
)

// ResolveDateRange reports which date filter should be applied. An explicit
// start_date/end_date window takes precedence over the single `date` value so
// that sending both is deterministic.
func (f *FilterParams) ResolveDateRange() (DateRangeMode, string, string) {
	if f == nil {
		return DateRangeNone, "", ""
	}
	switch {
	case f.StartDate != "" && f.EndDate != "":
		return DateRangeBetween, f.StartDate, f.EndDate
	case f.StartDate != "":
		return DateRangeFrom, f.StartDate, ""
	case f.EndDate != "":
		return DateRangeTo, "", f.EndDate
	case f.Data != "":
		return DateRangeExact, f.Data, ""
	default:
		return DateRangeNone, "", ""
	}
}

type customSystemLogLogicModel interface {
	RecordUnmatchedPayment(ctx context.Context, userID int64, payment UnmatchedPayment) error
	FilterSystemLog(ctx context.Context, filter *FilterParams) ([]*SystemLog, int64, error)
	FindFirstByDateType(ctx context.Context, date string, typ uint8) (*SystemLog, error)
	FindByDatesType(ctx context.Context, dates []string, typ uint8) ([]*SystemLog, error)
}

func (m *customSystemLogModel) RecordUnmatchedPayment(ctx context.Context, userID int64, payment UnmatchedPayment) error {
	content, err := json.Marshal(payment)
	if err != nil {
		return err
	}
	existing, err := m.db.SystemLog.Query().Where(entsystemlog.TypeEQ(TypeUnmatchedPayment.Uint8()), entsystemlog.ContentContains(payment.TradeNo)).All(ctx)
	if err != nil {
		return err
	}
	for _, item := range existing {
		var receipt UnmatchedPayment
		if json.Unmarshal([]byte(item.Content), &receipt) == nil && receipt.Platform == payment.Platform && receipt.TradeNo == payment.TradeNo {
			return nil
		}
	}
	return m.Insert(ctx, &SystemLog{Type: TypeUnmatchedPayment.Uint8(), Date: time.Now().Format(time.DateOnly), ObjectID: userID, Content: string(content)})
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
	if filter.Size > 100 {
		filter.Size = 100
	}

	query := m.db.SystemLog.Query()
	if filter.Type != 0 {
		query = query.Where(entsystemlog.TypeEQ(filter.Type))
	}

	switch mode, start, end := filter.ResolveDateRange(); mode {
	case DateRangeBetween:
		query = query.Where(entsystemlog.DateGTE(start), entsystemlog.DateLTE(end))
	case DateRangeFrom:
		query = query.Where(entsystemlog.DateGTE(start))
	case DateRangeTo:
		query = query.Where(entsystemlog.DateLTE(end))
	case DateRangeExact:
		query = query.Where(entsystemlog.DateEQ(start))
	}
	if filter.ObjectID != 0 {
		query = query.Where(entsystemlog.ObjectIDEQ(filter.ObjectID))
	}
	if filter.Search != "" {
		query = query.Where(entsystemlog.ContentContains(filter.Search))
	}
	for key, value := range filter.ContentInt64 {
		query = query.Where(func(selector *entsql.Selector) {
			if selector.Dialect() == dialect.Postgres {
				selector.Where(entsql.P(func(builder *entsql.Builder) {
					builder.WriteString("(").Ident(entsystemlog.FieldContent).WriteString("::jsonb ->> ").Arg(key).WriteString(")::bigint = ").Arg(value)
				}))
				return
			}
			selector.Where(sqljson.ValueEQ(entsystemlog.FieldContent, value, sqljson.Path(key)))
		})
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	offset := (filter.Page - 1) * filter.Size
	if filter.Offset != nil && *filter.Offset >= 0 {
		offset = *filter.Offset
	}
	items, err := query.Order(ent.Desc(entsystemlog.FieldID)).Limit(filter.Size).Offset(offset).All(ctx)
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
