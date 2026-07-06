package user

import (
	"context"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/predicate"
	entuser "github.com/perfect-panel/server/ent/user"
	entauth "github.com/perfect-panel/server/ent/userauthmethod"
	entsub "github.com/perfect-panel/server/ent/usersubscribe"
)

func (m *customUserModel) QueryEmailRecipients(ctx context.Context, filter *EmailRecipientFilter) ([]string, error) {
	if filter != nil && filter.Scope == 5 {
		return nil, nil
	}
	var emails []string
	err := m.emailRecipientQuery(filter).Select(entauth.FieldAuthIdentifier).Scan(ctx, &emails)
	return emails, err
}

func (m *customUserModel) CountEmailRecipients(ctx context.Context, filter *EmailRecipientFilter) (int64, error) {
	if filter != nil && filter.Scope == 5 {
		return 0, nil
	}
	count, err := m.emailRecipientQuery(filter).Count(ctx)
	return int64(count), err
}

func (m *customUserModel) emailRecipientQuery(filter *EmailRecipientFilter) *ent.UserAuthMethodQuery {
	if filter == nil {
		filter = &EmailRecipientFilter{Scope: 1}
	}
	query := m.db.UserAuthMethod.Query().Where(entauth.AuthType("email"), predicate.UserAuthMethod(func(s *entsql.Selector) {
		userTable := entsql.Table(entuser.Table)
		s.Join(userTable).On(s.C(entauth.FieldUserID), userTable.C(entuser.FieldID))
		if filter.RegisterStartTime != 0 {
			s.Where(entsql.GTE(userTable.C(entuser.FieldCreatedAt), time.UnixMilli(filter.RegisterStartTime)))
		}
		if filter.RegisterEndTime != 0 {
			s.Where(entsql.LTE(userTable.C(entuser.FieldCreatedAt), time.UnixMilli(filter.RegisterEndTime)))
		}
		subTable := entsql.Table(entsub.Table)
		switch filter.Scope {
		case 2:
			s.Join(subTable).On(userTable.C(entuser.FieldID), subTable.C(entsub.FieldUserID))
			s.Where(entsql.In(subTable.C(entsub.FieldStatus), 1, 2))
		case 3:
			s.Join(subTable).On(userTable.C(entuser.FieldID), subTable.C(entsub.FieldUserID))
			s.Where(entsql.EQ(subTable.C(entsub.FieldStatus), 3))
		case 4:
			s.LeftJoin(subTable).On(userTable.C(entuser.FieldID), subTable.C(entsub.FieldUserID))
			s.Where(entsql.IsNull(subTable.C(entsub.FieldUserID)))
		}
	}))
	return query
}
