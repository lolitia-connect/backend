package log

import (
	"context"

	"github.com/perfect-panel/server/ent"
)

var _ Model = (*customSystemLogModel)(nil)

type (
	Model interface {
		systemLogModel
		customSystemLogLogicModel
	}
	systemLogModel interface {
		Insert(ctx context.Context, data *SystemLog) error
		FindOne(ctx context.Context, id int64) (*SystemLog, error)
		Update(ctx context.Context, data *SystemLog) error
		Delete(ctx context.Context, id int64) error
	}
	customSystemLogModel struct {
		*defaultLogModel
	}
	defaultLogModel struct {
		db *ent.Client
	}
)

func newSystemLogModel(db *ent.Client) *defaultLogModel {
	return &defaultLogModel{
		db: db,
	}
}

func (m *defaultLogModel) Insert(ctx context.Context, data *SystemLog) error {
	created, err := m.db.SystemLog.Create().
		SetType(data.Type).
		SetDate(data.Date).
		SetObjectID(data.ObjectID).
		SetContent(data.Content).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, created)
	return nil
}

func (m *defaultLogModel) FindOne(ctx context.Context, id int64) (*SystemLog, error) {
	data, err := m.db.SystemLog.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return logFromEnt(data), nil
}

func (m *defaultLogModel) Update(ctx context.Context, data *SystemLog) error {
	updated, err := m.db.SystemLog.UpdateOneID(data.Id).
		SetType(data.Type).
		SetDate(data.Date).
		SetObjectID(data.ObjectID).
		SetContent(data.Content).
		Save(ctx)
	if err != nil {
		return err
	}
	copyFromEnt(data, updated)
	return nil
}

func (m *defaultLogModel) Delete(ctx context.Context, id int64) error {
	return m.db.SystemLog.DeleteOneID(id).Exec(ctx)
}

func logFromEnt(data *ent.SystemLog) *SystemLog {
	if data == nil {
		return nil
	}
	return &SystemLog{
		Id:        data.ID,
		Type:      data.Type,
		Date:      data.Date,
		ObjectID:  data.ObjectID,
		Content:   data.Content,
		CreatedAt: data.CreatedAt,
	}
}

func copyFromEnt(dst *SystemLog, src *ent.SystemLog) {
	if dst == nil || src == nil {
		return
	}
	*dst = *logFromEnt(src)
}
