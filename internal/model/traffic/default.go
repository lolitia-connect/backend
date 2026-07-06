package traffic

import (
	"context"

	"github.com/perfect-panel/server/ent"
	enttrafficlog "github.com/perfect-panel/server/ent/trafficlog"
)

var _ Model = (*customTrafficModel)(nil)

type (
	Model interface {
		trafficModel
		customTrafficLogicModel
	}
	trafficModel interface {
		Insert(ctx context.Context, data *TrafficLog) error
		FindOne(ctx context.Context, id int64) (*TrafficLog, error)
		Update(ctx context.Context, data *TrafficLog) error
		Delete(ctx context.Context, id int64) error
	}

	customTrafficModel struct {
		*defaultTrafficModel
	}
	defaultTrafficModel struct {
		db    *ent.Client
		table string
	}
)

func newTrafficModel(db *ent.Client) *defaultTrafficModel {
	return &defaultTrafficModel{
		db:    db,
		table: "traffic",
	}
}

func (m *defaultTrafficModel) Insert(ctx context.Context, data *TrafficLog) error {
	create := m.db.TrafficLog.Create().
		SetServerID(data.ServerId).
		SetUserID(data.UserId).
		SetSubscribeID(data.SubscribeId).
		SetDownload(data.Download).
		SetUpload(data.Upload)
	if !data.Timestamp.IsZero() {
		create.SetTimestamp(data.Timestamp)
	}
	created, err := create.Save(ctx)
	if err != nil {
		return err
	}
	data.Id = created.ID
	return nil
}

func (m *defaultTrafficModel) FindOne(ctx context.Context, id int64) (*TrafficLog, error) {
	data, err := m.db.TrafficLog.Query().Where(enttrafficlog.ID(id)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return entToTrafficLog(data), nil
}

func (m *defaultTrafficModel) Update(ctx context.Context, data *TrafficLog) error {
	return m.db.TrafficLog.UpdateOneID(data.Id).
		SetServerID(data.ServerId).
		SetUserID(data.UserId).
		SetSubscribeID(data.SubscribeId).
		SetDownload(data.Download).
		SetUpload(data.Upload).
		SetTimestamp(data.Timestamp).
		Exec(ctx)
}

func (m *defaultTrafficModel) Delete(ctx context.Context, id int64) error {
	err := m.db.TrafficLog.DeleteOneID(id).Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

func entToTrafficLog(data *ent.TrafficLog) *TrafficLog {
	if data == nil {
		return nil
	}
	return &TrafficLog{
		Id:          data.ID,
		ServerId:    data.ServerID,
		UserId:      data.UserID,
		SubscribeId: data.SubscribeID,
		Download:    data.Download,
		Upload:      data.Upload,
		Timestamp:   data.Timestamp,
	}
}
