package user

import (
	"context"
	"time"

	"github.com/perfect-panel/server/ent"
	entdevice "github.com/perfect-panel/server/ent/userdevice"
	entrecord "github.com/perfect-panel/server/ent/userdeviceonlinerecord"
)

func (m *customUserModel) FindOneDevice(ctx context.Context, id int64) (*Device, error) {
	item, err := m.db.UserDevice.Get(ctx, id)
	switch {
	case err == nil:
		return entToDevice(item), nil
	default:
		return nil, err
	}
}

func (m *customUserModel) FindOneDeviceByIdentifier(ctx context.Context, id string) (*Device, error) {
	item, err := m.db.UserDevice.Query().Where(entdevice.Identifier(id)).First(ctx)
	switch {
	case err == nil:
		return entToDevice(item), nil
	default:
		return nil, err
	}
}

// QueryDevicePageList  returns a list of records that meet the conditions.
func (m *customUserModel) QueryDevicePageList(ctx context.Context, userId, subscribeId int64, page, size int) ([]*Device, int64, error) {
	query := m.db.UserDevice.Query().Where(entdevice.UserID(userId))
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	items, err := query.Offset((page - 1) * size).Limit(size).All(ctx)
	return entDevicesToPtrModels(items), int64(total), err
}

// QueryDeviceList  returns a list of records that meet the conditions.
func (m *customUserModel) QueryDeviceList(ctx context.Context, userId int64) ([]*Device, int64, error) {
	query := m.db.UserDevice.Query().Where(entdevice.UserID(userId))
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	items, err := query.All(ctx)
	return entDevicesToPtrModels(items), int64(total), err
}

func (m *customUserModel) UpdateDevice(ctx context.Context, data *Device) error {
	_, err := m.FindOneDevice(ctx, data.Id)
	if err != nil {
		return err
	}
	updated, err := deviceUpdate(m.db.UserDevice.UpdateOneID(data.Id), data).Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToDevice(updated)
	return nil
}

func (m *customUserModel) DeleteDevice(ctx context.Context, id int64) error {
	_, err := m.FindOneDevice(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err = m.db.UserDevice.DeleteOneID(id).Exec(ctx); err != nil {
		return err
	}
	return nil
}

func (m *customUserModel) InsertDevice(ctx context.Context, data *Device) error {
	created, err := deviceCreate(m.db.UserDevice.Create(), data).Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToDevice(created)
	return nil
}

func (m *customUserModel) FindDeviceOnlineRecord(ctx context.Context, userId int64, startTime, endTime string) (*DeviceOnlineRecord, error) {
	item, err := m.db.UserDeviceOnlineRecord.Query().Where(entrecord.UserID(userId), entrecord.CreatedAtGTE(parseTime(startTime)), entrecord.CreatedAtLT(parseTime(endTime))).First(ctx)
	return entToDeviceOnlineRecord(item), err
}

func (m *customUserModel) FindMaxDeviceOnlineSeconds(ctx context.Context, userId int64) (int64, error) {
	item, err := m.db.UserDeviceOnlineRecord.Query().Where(entrecord.UserID(userId)).Order(ent.Desc(entrecord.FieldOnlineSeconds)).First(ctx)
	if ent.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return item.OnlineSeconds, nil
}

func (m *customUserModel) FindMaxDeviceDurationDays(ctx context.Context, userId int64) (int64, error) {
	item, err := m.db.UserDeviceOnlineRecord.Query().Where(entrecord.UserID(userId)).Order(ent.Desc(entrecord.FieldDurationDays)).First(ctx)
	if ent.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return int64(item.DurationDays), nil
}

func (m *customUserModel) FindDeviceOnlineRecordsSince(ctx context.Context, userId int64, since time.Time) ([]*DeviceOnlineRecord, error) {
	items, err := m.db.UserDeviceOnlineRecord.Query().Where(entrecord.UserID(userId), entrecord.CreatedAtGTE(since)).Order(ent.Asc(entrecord.FieldCreatedAt)).All(ctx)
	return entDeviceOnlineRecordsToModels(items), err
}

func (m *customUserModel) InsertDeviceOnlineRecord(ctx context.Context, data *DeviceOnlineRecord) error {
	c := m.db.UserDeviceOnlineRecord.Create().SetUserID(data.UserId).SetIdentifier(data.Identifier).SetOnlineTime(data.OnlineTime).SetOfflineTime(data.OfflineTime).SetOnlineSeconds(data.OnlineSeconds).SetDurationDays(data.DurationDays)
	if data.Id > 0 {
		c.SetID(data.Id)
	}
	created, err := c.Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToDeviceOnlineRecord(created)
	return nil
}

func parseTime(value string) time.Time { t, _ := time.Parse(time.RFC3339, value); return t }
