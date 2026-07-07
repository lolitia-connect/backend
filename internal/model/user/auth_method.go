package user

import (
	"context"

	"github.com/perfect-panel/server/ent"
	entauth "github.com/perfect-panel/server/ent/userauthmethod"
)

func (m *defaultUserModel) FindUserAuthMethods(ctx context.Context, userId int64) ([]*AuthMethods, error) {
	items, err := m.db.UserAuthMethod.Query().Where(entauth.UserID(userId)).All(ctx)
	return entAuthMethodsToPtrModels(items), err
}

func (m *defaultUserModel) CountUserAuthMethods(ctx context.Context, userId int64) (int64, error) {
	count, err := m.db.UserAuthMethod.Query().Where(entauth.UserID(userId)).Count(ctx)
	return int64(count), err
}

func (m *defaultUserModel) FindFirstUserAuthMethodByTypes(ctx context.Context, userId int64, authTypes ...string) (*AuthMethods, error) {
	item, err := m.db.UserAuthMethod.Query().Where(entauth.UserID(userId), entauth.AuthTypeIn(authTypes...)).First(ctx)
	return entToAuthMethod(item), err
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
	created, err := authMethodCreate(m.db.UserAuthMethod.Create(), data).Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToAuthMethod(created)
	return nil
}

func (m *defaultUserModel) UpdateUserAuthMethods(ctx context.Context, data *AuthMethods) error {
	if data.Id > 0 {
		return authMethodUpdate(m.db.UserAuthMethod.UpdateOneID(data.Id), data).Exec(ctx)
	} else {
		_, err := m.db.UserAuthMethod.Update().Where(entauth.UserID(data.UserId), entauth.AuthType(data.AuthType)).SetAuthIdentifier(data.AuthIdentifier).SetVerified(data.Verified).Save(ctx)
		return err
	}
}

func (m *defaultUserModel) DeleteUserAuthMethods(ctx context.Context, userId int64, platform string) error {
	_, err := m.db.UserAuthMethod.Delete().Where(entauth.UserID(userId), entauth.AuthType(platform)).Exec(ctx)
	return err
}

func (m *defaultUserModel) UpdateUserAuthMethodOwner(ctx context.Context, authType, identifier string, userId int64) error {
	_, err := m.FindUserAuthMethodByOpenID(ctx, authType, identifier)
	if err != nil {
		return err
	}
	_, err = m.db.UserAuthMethod.Update().Where(entauth.AuthType(authType), entauth.AuthIdentifier(identifier)).SetUserID(userId).Save(ctx)
	return err
}

func (m *defaultUserModel) DeleteUserAuthMethodByIdentifier(ctx context.Context, authType, identifier string) error {
	_, err := m.FindUserAuthMethodByOpenID(ctx, authType, identifier)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return err
	}
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
