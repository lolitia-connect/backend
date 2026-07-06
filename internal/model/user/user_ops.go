package user

import (
	"context"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	entuser "github.com/perfect-panel/server/ent/user"
	entauth "github.com/perfect-panel/server/ent/userauthmethod"
)

// BatchDeleteUser deletes multiple records by primary key.
func (m *customUserModel) BatchDeleteUser(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	users, err := m.FindUsersByIds(ctx, ids)
	if err != nil {
		return err
	}
	_, err = m.db.User.Update().Where(entuser.IDIn(ids...)).SetDeletedAt(time.Now()).Save(ctx)
	if err != nil {
		return err
	}
	return m.GetCacheManager().ClearCache(ctx, m.batchGetCacheKeys(users...)...)
}

func (m *customUserModel) UpdateUserSubscribeWithTraffic(ctx context.Context, id, download, upload int64, isExpired bool) error {
	u := m.db.UserSubscribe.UpdateOneID(id)
	if isExpired {
		u.AddExpiredDownload(download).AddExpiredUpload(upload)
	} else {
		u.AddDownload(download).AddUpload(upload)
	}
	if err := u.Exec(ctx); err != nil {
		return err
	}
	if sub, err := m.FindOneSubscribe(ctx, id); err == nil {
		_ = m.ClearSubscribeCacheByModels(ctx, sub)
	}
	return nil
}

func (m *customUserModel) QueryAdminUsers(ctx context.Context) ([]*User, error) {
	items, err := m.db.User.Query().Where(entuser.IsAdmin(true), entuser.DeletedAtIsNil()).All(ctx)
	if err != nil {
		return nil, err
	}
	data := entUsersToModels(items)
	for _, item := range data {
		if auths, err := m.db.UserAuthMethod.Query().Where(entauth.UserID(item.Id)).Order(entauth.ByAuthType(entsql.OrderDesc())).All(ctx); err == nil {
			item.AuthMethods = entAuthMethodsToModels(auths)
		}
	}
	return data, nil
}

func (m *customUserModel) UpdateUserCache(ctx context.Context, data *User) error {
	return m.ClearUserCache(ctx, data)
}

func (m *customUserModel) FindOneByReferCode(ctx context.Context, referCode string) (*User, error) {
	item, err := m.db.User.Query().Where(entuser.ReferCode(referCode), entuser.DeletedAtIsNil()).First(ctx)
	return entToUser(item), err
}

func (m *customUserModel) FindOneSubscribeDetailsById(ctx context.Context, id int64) (*SubscribeDetails, error) {
	item, err := m.db.UserSubscribe.Get(ctx, id)
	data := entToSubscribeDetails(item)
	if err != nil || data == nil {
		return data, err
	}
	if err := m.hydrateSubscribeDetails(ctx, data); err != nil {
		return nil, err
	}
	return data, nil
}
