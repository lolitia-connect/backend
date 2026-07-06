package task

import (
	"context"
	"time"

	"github.com/perfect-panel/server/ent"
	enttask "github.com/perfect-panel/server/ent/task"
)

var _ Model = (*defaultTaskModel)(nil)

type Model interface {
	Insert(ctx context.Context, data *Task) error
	FindOne(ctx context.Context, id int64) (*Task, error)
	FindOneByType(ctx context.Context, id int64, typ Type) (*Task, error)
	QueryTaskList(ctx context.Context, filter *Filter) (int64, []*Task, error)
	Update(ctx context.Context, data *Task) error
	UpdateStatus(ctx context.Context, id int64, status int8) error
}

type Filter struct {
	Type   Type
	Page   int
	Size   int
	Status *uint8
	Scope  *int8
}

type defaultTaskModel struct {
	db *ent.Client
}

func NewModel(db *ent.Client) Model {
	return &defaultTaskModel{
		db: db,
	}
}

func (m *defaultTaskModel) Insert(ctx context.Context, data *Task) error {
	created, err := m.db.Task.Create().
		SetType(data.Type).
		SetScope(data.Scope).
		SetContent(data.Content).
		SetStatus(data.Status).
		SetErrors(data.Errors).
		SetTotal(data.Total).
		SetCurrent(data.Current).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, created)
	return nil
}

func (m *defaultTaskModel) FindOne(ctx context.Context, id int64) (*Task, error) {
	data, err := m.db.Task.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return taskFromEnt(data), nil
}

func (m *defaultTaskModel) FindOneByType(ctx context.Context, id int64, typ Type) (*Task, error) {
	data, err := m.db.Task.Query().Where(enttask.IDEQ(id), enttask.TypeEQ(int8(typ))).Only(ctx)
	if err != nil {
		return nil, err
	}
	return taskFromEnt(data), nil
}

func (m *defaultTaskModel) QueryTaskList(ctx context.Context, filter *Filter) (int64, []*Task, error) {
	var data []*Task
	if filter == nil {
		filter = &Filter{
			Type: Undefined,
			Page: 1,
			Size: 10,
		}
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.Size <= 0 {
		filter.Size = 10
	}

	query := m.db.Task.Query()
	if filter.Type != Undefined {
		query = query.Where(enttask.TypeEQ(int8(filter.Type)))
	}
	if filter.Status != nil {
		query = query.Where(enttask.StatusEQ(int8(*filter.Status)))
	}
	if filter.Scope != nil {
		all, err := query.Order(ent.Desc(enttask.FieldCreatedAt)).All(ctx)
		if err != nil {
			return 0, nil, err
		}

		// Scope is stored as JSON text; filter here to keep the query dialect-neutral.
		filtered := make([]*Task, 0, len(all))
		for _, item := range all {
			var scope EmailScope
			if err := scope.Unmarshal([]byte(item.Scope)); err != nil {
				continue
			}
			if scope.Type == *filter.Scope {
				filtered = append(filtered, taskFromEnt(item))
			}
		}

		total := int64(len(filtered))
		start := (filter.Page - 1) * filter.Size
		if start >= len(filtered) {
			return total, []*Task{}, nil
		}
		end := start + filter.Size
		if end > len(filtered) {
			end = len(filtered)
		}
		return total, filtered[start:end], nil
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return 0, nil, err
	}
	items, err := query.
		Order(ent.Desc(enttask.FieldCreatedAt)).
		Offset((filter.Page - 1) * filter.Size).
		Limit(filter.Size).
		All(ctx)
	if err != nil {
		return 0, nil, err
	}
	data = make([]*Task, 0, len(items))
	for _, item := range items {
		data = append(data, taskFromEnt(item))
	}
	return int64(total), data, nil
}

func (m *defaultTaskModel) Update(ctx context.Context, data *Task) error {
	updated, err := m.db.Task.UpdateOneID(data.Id).
		SetType(data.Type).
		SetScope(data.Scope).
		SetContent(data.Content).
		SetStatus(data.Status).
		SetErrors(data.Errors).
		SetTotal(data.Total).
		SetCurrent(data.Current).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, updated)
	return nil
}

func (m *defaultTaskModel) UpdateStatus(ctx context.Context, id int64, status int8) error {
	return m.db.Task.UpdateOneID(id).SetStatus(status).SetUpdatedAt(time.Now()).Exec(ctx)
}

func taskFromEnt(data *ent.Task) *Task {
	if data == nil {
		return nil
	}
	return &Task{
		Id:        data.ID,
		Type:      data.Type,
		Scope:     data.Scope,
		Content:   data.Content,
		Status:    data.Status,
		Errors:    data.Errors,
		Total:     data.Total,
		Current:   data.Current,
		CreatedAt: data.CreatedAt,
		UpdatedAt: data.UpdatedAt,
	}
}

func copyFromEnt(dst *Task, src *ent.Task) {
	if dst == nil || src == nil {
		return
	}
	*dst = *taskFromEnt(src)
}
