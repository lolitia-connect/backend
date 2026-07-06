package user

import (
	"context"
	"fmt"
	"time"

	"github.com/perfect-panel/server/ent"
	entuser "github.com/perfect-panel/server/ent/user"
	entauth "github.com/perfect-panel/server/ent/userauthmethod"
	entdevice "github.com/perfect-panel/server/ent/userdevice"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/redis/go-redis/v9"
)

var (
	cacheUserIdPrefix    = "cache:user:id:"
	cacheUserEmailPrefix = "cache:user:email:"
)
var _ Model = (*customUserModel)(nil)

type (
	Model interface {
		userModel
		customUserLogicModel
	}
	userModel interface {
		Insert(ctx context.Context, data *User) error
		FindOne(ctx context.Context, id int64) (*User, error)
		Update(ctx context.Context, data *User) error
		Delete(ctx context.Context, id int64) error
		Transaction(ctx context.Context, fn func(db *ent.Client) error) error
	}

	customUserModel struct {
		*defaultUserModel
	}
	defaultUserModel struct {
		db    *ent.Client
		redis *redis.Client
		table string
	}
)

func newUserModel(db *ent.Client, c *redis.Client) *defaultUserModel {
	return &defaultUserModel{
		db:    db,
		redis: c,
		table: "user",
	}
}

func (m *defaultUserModel) batchGetCacheKeys(users ...*User) []string {
	var keys []string
	for _, user := range users {
		keys = append(keys, user.GetCacheKeys()...)
	}
	return keys
}

func (m *defaultUserModel) getCacheKeys(data *User) []string {
	if data == nil {
		return []string{}
	}
	return data.GetCacheKeys()
}

func (m *defaultUserModel) clearUserCache(ctx context.Context, data ...*User) error {
	return m.ClearUserCache(ctx, data...)
}

func (m *defaultUserModel) FindOneByEmail(ctx context.Context, email string) (*User, error) {
	key := fmt.Sprintf("%s%v", cacheUserEmailPrefix, email)
	var cached User
	if err := getJSONCache(ctx, m.redis, key, &cached); err == nil {
		return &cached, nil
	}
	auth, err := m.db.UserAuthMethod.Query().Where(entauth.AuthType("email"), entauth.AuthIdentifier(email)).First(ctx)
	if err != nil {
		return nil, err
	}
	data, err := m.findOne(ctx, auth.UserID, true)
	if err == nil {
		_ = setJSONCache(ctx, m.redis, key, data)
	}
	return data, err
}

func (m *defaultUserModel) Insert(ctx context.Context, data *User) error {
	created, err := userCreate(m.db.User.Create(), data).Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToUser(created)
	return m.ClearUserCache(ctx, data)
}

func (m *defaultUserModel) FindOne(ctx context.Context, id int64) (*User, error) {
	userIdKey := fmt.Sprintf("%s%v", cacheUserIdPrefix, id)
	var cached User
	if err := getJSONCache(ctx, m.redis, userIdKey, &cached); err == nil {
		return &cached, nil
	}
	resp, err := m.findOne(ctx, id, true)
	if err == nil {
		_ = setJSONCache(ctx, m.redis, userIdKey, resp)
	}
	return resp, err
}

func (m *defaultUserModel) Update(ctx context.Context, data *User) error {
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	updated, err := userUpdate(m.db.User.UpdateOneID(data.Id), data).Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToUser(updated)
	return m.GetCacheManager().ClearCache(ctx, append(m.getCacheKeys(old), m.getCacheKeys(data)...)...)
}

func (m *defaultUserModel) Delete(ctx context.Context, id int64) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return err
	}

	// Use batch related cache cleaning, including a cache of all relevant data
	defer func() {
		if clearErr := m.BatchClearRelatedCache(ctx, data); clearErr != nil {
			// Record cache cleaning errors, but do not block deletion operations
			logger.Errorf("failed to clear related cache for user %d: %v", id, clearErr.Error())
		}
	}()

	return m.db.User.UpdateOneID(id).SetDeletedAt(time.Now()).Exec(ctx)
}

func (m *defaultUserModel) Transaction(ctx context.Context, fn func(db *ent.Client) error) error {
	tx, err := m.db.Tx(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx.Client()); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (m *defaultUserModel) findOne(ctx context.Context, id int64, unscoped bool) (*User, error) {
	q := m.db.User.Query().Where(entuser.ID(id))
	if !unscoped {
		q = q.Where(entuser.DeletedAtIsNil())
	}
	entUser, err := q.First(ctx)
	if err != nil {
		return nil, err
	}
	data := entToUser(entUser)
	if auths, err := m.db.UserAuthMethod.Query().Where(entauth.UserID(id)).Order(entauth.ByAuthType()).All(ctx); err == nil {
		data.AuthMethods = entAuthMethodsToModels(auths)
	}
	if devices, err := m.db.UserDevice.Query().Where(entdevice.UserID(id)).All(ctx); err == nil {
		data.UserDevices = entDevicesToModels(devices)
	}
	return data, nil
}
