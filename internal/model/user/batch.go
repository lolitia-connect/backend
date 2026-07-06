package user

import (
	"context"

	entuser "github.com/perfect-panel/server/ent/user"
	entsub "github.com/perfect-panel/server/ent/usersubscribe"
)

func (m *customUserModel) FindUsersByIds(ctx context.Context, ids []int64) ([]*User, error) {
	var users []*User
	if len(ids) == 0 {
		return users, nil
	}
	items, err := m.db.User.Query().Where(entuser.IDIn(ids...)).All(ctx)
	return entUsersToModels(items), err
}

func (m *customUserModel) FindSubscribesByIds(ctx context.Context, ids []int64) ([]*Subscribe, error) {
	var subscribes []*Subscribe
	if len(ids) == 0 {
		return subscribes, nil
	}
	items, err := m.db.UserSubscribe.Query().Where(entsub.IDIn(ids...)).All(ctx)
	return entSubscribesToModels(items), err
}
