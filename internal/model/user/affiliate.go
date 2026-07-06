package user

import (
	"context"

	entsql "entgo.io/ent/dialect/sql"
	entuser "github.com/perfect-panel/server/ent/user"
	entauth "github.com/perfect-panel/server/ent/userauthmethod"
)

func (m *customUserModel) CountAffiliates(ctx context.Context, refererId int64) (int64, error) {
	count, err := m.db.User.Query().Where(entuser.RefererID(refererId), entuser.DeletedAtIsNil()).Count(ctx)
	return int64(count), err
}

func (m *customUserModel) QueryAffiliateList(ctx context.Context, refererId int64, page, size int) ([]*User, int64, error) {
	query := m.db.User.Query().Where(entuser.RefererID(refererId), entuser.DeletedAtIsNil())
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	items, err := query.Order(entuser.ByID(entsql.OrderDesc())).Offset((page - 1) * size).Limit(size).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	list := entUsersToModels(items)
	for _, item := range list {
		if auths, err := m.db.UserAuthMethod.Query().Where(entauth.UserID(item.Id)).All(ctx); err == nil {
			item.AuthMethods = entAuthMethodsToModels(auths)
		}
	}
	return list, int64(total), nil
}
