package user

import (
	"context"
	"strings"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/predicate"
	entuser "github.com/perfect-panel/server/ent/user"
	entauth "github.com/perfect-panel/server/ent/userauthmethod"
	entdevice "github.com/perfect-panel/server/ent/userdevice"
	entsub "github.com/perfect-panel/server/ent/usersubscribe"
	"github.com/perfect-panel/server/pkg/orm"
)

// QueryPageList returns a list of records that meet the conditions.
func (m *customUserModel) QueryPageList(ctx context.Context, page, size int, filter *UserFilterParams) ([]*User, int64, error) {
	query := applyUserPageFilters(m.db.User.Query(), filter)
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	if filter != nil && filter.Order != "" && strings.EqualFold(filter.Order, "ASC") {
		query = query.Order(entuser.ByID())
	} else {
		query = query.Order(entuser.ByID(entsql.OrderDesc()))
	}
	items, err := query.Offset((page - 1) * size).Limit(size).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	list := entUsersToModels(items)
	for _, item := range list {
		if auths, err := m.db.UserAuthMethod.Query().Where(entauth.UserID(item.Id)).All(ctx); err == nil {
			item.AuthMethods = entAuthMethodsToModels(auths)
		}
		if devices, err := m.db.UserDevice.Query().Where(entdevice.UserID(item.Id)).All(ctx); err == nil {
			item.UserDevices = entDevicesToModels(devices)
		}
	}
	return list, int64(total), nil
}

func applyUserPageFilters(query *ent.UserQuery, filter *UserFilterParams) *ent.UserQuery {
	if filter == nil {
		return query.Where(entuser.DeletedAtIsNil())
	}
	if !filter.Unscoped {
		query = query.Where(entuser.DeletedAtIsNil())
	}
	if filter.UserId != nil {
		query = query.Where(entuser.ID(*filter.UserId))
	}
	if filter.Search != "" {
		search := orm.LikePrefixPattern(filter.Search)
		if search != "" {
			query = query.Where(userSearchPredicate(search))
		}
	}
	if filter.UserSubscribeId != nil {
		query = query.Where(userSubscribeExistsPredicate(entsub.FieldID, *filter.UserSubscribeId))
	}
	if filter.SubscribeId != nil {
		query = query.Where(userSubscribeExistsPredicate(entsub.FieldSubscribeID, *filter.SubscribeId))
	}
	return query
}

func userSearchPredicate(search string) predicate.User {
	return predicate.User(func(s *entsql.Selector) {
		authTable := entsql.Table(entauth.Table)
		s.Where(entsql.Or(
			entsql.P(func(b *entsql.Builder) {
				b.Ident(s.C(entuser.FieldReferCode)).WriteString(" LIKE ").Arg(search).WriteString(orm.LikeEscapeClause())
			}),
			entsql.Exists(entsql.Select().From(authTable).Where(entsql.And(
				entsql.ColumnsEQ(authTable.C(entauth.FieldUserID), s.C(entuser.FieldID)),
				entsql.P(func(b *entsql.Builder) {
					b.Ident(authTable.C(entauth.FieldAuthIdentifier)).WriteString(" LIKE ").Arg(search).WriteString(orm.LikeEscapeClause())
				}),
			))),
		))
	})
}

func userSubscribeExistsPredicate(field string, value int64) predicate.User {
	return predicate.User(func(s *entsql.Selector) {
		subTable := entsql.Table(entsub.Table)
		s.Where(entsql.Exists(entsql.Select().From(subTable).Where(entsql.And(
			entsql.ColumnsEQ(subTable.C(entsub.FieldUserID), s.C(entuser.FieldID)),
			entsql.EQ(subTable.C(field), value),
			entsql.In(subTable.C(entsub.FieldStatus), 0, 1),
		))))
	})
}
