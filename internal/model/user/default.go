package user

import (
	"context"
	"time"

	"github.com/perfect-panel/server/ent"
	entuser "github.com/perfect-panel/server/ent/user"
	entauth "github.com/perfect-panel/server/ent/userauthmethod"
	entdevice "github.com/perfect-panel/server/ent/userdevice"
	"github.com/redis/go-redis/v9"
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

func (m *defaultUserModel) FindOneByEmail(ctx context.Context, email string) (*User, error) {
	auth, err := m.db.UserAuthMethod.Query().Where(entauth.AuthType("email"), entauth.AuthIdentifier(email)).First(ctx)
	if err != nil {
		return nil, err
	}
	return m.findOne(ctx, auth.UserID, true)
}

func (m *defaultUserModel) Insert(ctx context.Context, data *User) error {
	created, err := userCreate(m.db.User.Create(), data).Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToUser(created)
	return nil
}

func (m *defaultUserModel) FindOne(ctx context.Context, id int64) (*User, error) {
	return m.findOne(ctx, id, true)
}

func (m *defaultUserModel) Update(ctx context.Context, data *User) error {
	_, err := m.FindOne(ctx, data.Id)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	updated, err := userUpdate(m.db.User.UpdateOneID(data.Id), data).Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToUser(updated)
	return nil
}

func (m *defaultUserModel) Delete(ctx context.Context, id int64) error {
	_, err := m.FindOne(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return err
	}

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
