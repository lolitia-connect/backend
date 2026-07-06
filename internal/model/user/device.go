package user

import (
	"context"
	"fmt"
	"time"

	"github.com/perfect-panel/server/ent"
	entdevice "github.com/perfect-panel/server/ent/userdevice"
	entrecord "github.com/perfect-panel/server/ent/userdeviceonlinerecord"
)

func (m *customUserModel) FindOneDevice(ctx context.Context, id int64) (*Device, error) {
	deviceIdKey := fmt.Sprintf("%s%v", cacheUserDeviceIdPrefix, id)
	var resp Device
	if err := getJSONCache(ctx, m.redis, deviceIdKey, &resp); err == nil {
		return &resp, nil
	}
	item, err := m.db.UserDevice.Get(ctx, id)
	resp = *entToDevice(item)
	if err == nil {
		_ = setJSONCache(ctx, m.redis, deviceIdKey, &resp)
	}
	switch {
	case err == nil:
		return &resp, nil
	default:
		return nil, err
	}
}

func (m *customUserModel) FindOneDeviceByIdentifier(ctx context.Context, id string) (*Device, error) {
	deviceIdKey := fmt.Sprintf("%s%v", cacheUserDeviceNumberPrefix, id)
	var resp Device
	if err := getJSONCache(ctx, m.redis, deviceIdKey, &resp); err == nil {
		return &resp, nil
	}
	item, err := m.db.UserDevice.Query().Where(entdevice.Identifier(id)).First(ctx)
	resp = *entToDevice(item)
	if err == nil {
		_ = setJSONCache(ctx, m.redis, deviceIdKey, &resp)
	}
	switch {
	case err == nil:
		return &resp, nil
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
	old, err := m.FindOneDevice(ctx, data.Id)
	if err != nil {
		return err
	}
	updated, err := deviceUpdate(m.db.UserDevice.UpdateOneID(data.Id), data).Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToDevice(updated)
	return m.GetCacheManager().ClearCache(ctx, old.GetCacheKeys()...)
}

func (m *customUserModel) DeleteDevice(ctx context.Context, id int64) error {
	data, err := m.FindOneDevice(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err = m.db.UserDevice.DeleteOneID(id).Exec(ctx); err != nil {
		return err
	}
	return m.GetCacheManager().ClearCache(ctx, data.GetCacheKeys()...)
}

func (m *customUserModel) InsertDevice(ctx context.Context, data *Device) error {
	defer func() {
		if clearErr := m.ClearDeviceCache(ctx, data); clearErr != nil {
			// log cache clear error
		}
	}()

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
