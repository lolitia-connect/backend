package user

import (
	"context"

	"github.com/perfect-panel/server/ent"
	entauth "github.com/perfect-panel/server/ent/userauthmethod"
	"github.com/perfect-panel/server/pkg/logger"
)

func (m *defaultUserModel) FindUserAuthMethods(ctx context.Context, userId int64) ([]*AuthMethods, error) {
	items, err := m.db.UserAuthMethod.Query().Where(entauth.UserID(userId)).All(ctx)
	return entAuthMethodsToPtrModels(items), err
}

func (m *defaultUserModel) FindUserAuthMethodByOpenID(ctx context.Context, method, openID string) (*AuthMethods, error) {
	item, err := m.db.UserAuthMethod.Query().Where(entauth.AuthType(method), entauth.AuthIdentifier(openID)).First(ctx)
	return entToAuthMethod(item), err
}

func (m *defaultUserModel) FindUserAuthMethodByPlatform(ctx context.Context, userId int64, platform string) (*AuthMethods, error) {
	item, err := m.db.UserAuthMethod.Query().Where(entauth.UserID(userId), entauth.AuthType(platform)).First(ctx)
	return entToAuthMethod(item), err
}

func (m *defaultUserModel) InsertUserAuthMethods(ctx context.Context, data *AuthMethods) error {
	u, err := m.FindOne(ctx, data.UserId)
	if err != nil {
		return err
	}

	created, err := authMethodCreate(m.db.UserAuthMethod.Create(), data).Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToAuthMethod(created)
	return m.ClearUserCache(ctx, u)
}

func (m *defaultUserModel) UpdateUserAuthMethods(ctx context.Context, data *AuthMethods) error {
	u, err := m.FindOne(ctx, data.UserId)
	if err != nil {
		return err
	}

	if data.Id > 0 {
		err = authMethodUpdate(m.db.UserAuthMethod.UpdateOneID(data.Id), data).Exec(ctx)
	} else {
		_, err = m.db.UserAuthMethod.Update().Where(entauth.UserID(data.UserId), entauth.AuthType(data.AuthType)).SetAuthIdentifier(data.AuthIdentifier).SetVerified(data.Verified).Save(ctx)
	}
	if err != nil {
		return err
	}
	return m.ClearUserCache(ctx, u)
}

func (m *defaultUserModel) DeleteUserAuthMethods(ctx context.Context, userId int64, platform string) error {
	u, err := m.FindOne(ctx, userId)
	if err != nil {
		return err
	}
	defer func() {
		if err = m.ClearUserCache(context.Background(), u); err != nil {
			logger.Errorf("[UserModel] clear user cache failed: %v", err.Error())
		}
	}()
	_, err = m.db.UserAuthMethod.Delete().Where(entauth.UserID(userId), entauth.AuthType(platform)).Exec(ctx)
	return err
}

func (m *defaultUserModel) UpdateUserAuthMethodOwner(ctx context.Context, authType, identifier string, userId int64) error {
	authMethod, err := m.FindUserAuthMethodByOpenID(ctx, authType, identifier)
	if err != nil {
		return err
	}
	oldUser, err := m.FindOne(ctx, authMethod.UserId)
	if err != nil {
		return err
	}
	newUser, err := m.FindOne(ctx, userId)
	if err != nil {
		return err
	}
	defer func() {
		if err = m.ClearUserCache(context.Background(), oldUser, newUser); err != nil {
			logger.Errorf("[UserModel] clear user cache failed: %v", err.Error())
		}
	}()
	_, err = m.db.UserAuthMethod.Update().Where(entauth.AuthType(authType), entauth.AuthIdentifier(identifier)).SetUserID(userId).Save(ctx)
	return err
}

func (m *defaultUserModel) DeleteUserAuthMethodByIdentifier(ctx context.Context, authType, identifier string) error {
	authMethod, err := m.FindUserAuthMethodByOpenID(ctx, authType, identifier)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return err
	}
	u, err := m.FindOne(ctx, authMethod.UserId)
	if err != nil {
		return err
	}
	defer func() {
		if err = m.ClearUserCache(context.Background(), u); err != nil {
			logger.Errorf("[UserModel] clear user cache failed: %v", err.Error())
		}
	}()
	_, err = m.db.UserAuthMethod.Delete().Where(entauth.AuthType(authType), entauth.AuthIdentifier(identifier)).Exec(ctx)
	return err
}

func (m *defaultUserModel) UpsertUserAuthMethod(ctx context.Context, data *AuthMethods) error {
	current, err := m.FindUserAuthMethodByPlatform(ctx, data.UserId, data.AuthType)
	if err != nil {
		if ent.IsNotFound(err) {
			return m.InsertUserAuthMethods(ctx, data)
		}
		return err
	}
	current.AuthIdentifier = data.AuthIdentifier
	return m.UpdateUserAuthMethods(ctx, current)
}

func (m *defaultUserModel) FindUserAuthMethodByUserId(ctx context.Context, method string, userId int64) (*AuthMethods, error) {
	item, err := m.db.UserAuthMethod.Query().Where(entauth.AuthType(method), entauth.UserID(userId)).First(ctx)
	return entToAuthMethod(item), err
}
