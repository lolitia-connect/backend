package user

import (
	"context"
	"fmt"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	entorder "github.com/perfect-panel/server/ent/order"
	"github.com/perfect-panel/server/ent/predicate"
	entuser "github.com/perfect-panel/server/ent/user"
	entauth "github.com/perfect-panel/server/ent/userauthmethod"
	entdevice "github.com/perfect-panel/server/ent/userdevice"
	entsub "github.com/perfect-panel/server/ent/usersubscribe"
	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/pkg/orm"

	"github.com/redis/go-redis/v9"
)

const (
	cacheUserSubscribeTokenPrefix = "cache:user:subscribe:token:"
	cacheUserSubscribeUserPrefix  = "cache:user:subscribe:user:"
	cacheUserSubscribeIdPrefix    = "cache:user:subscribe:id:"
	cacheUserDeviceNumberPrefix   = "cache:user:device:number:"
	cacheUserDeviceIdPrefix       = "cache:user:device:id:"
)

type SubscribeDetails struct {
	Id               int64
	UserId           int64
	User             *User
	OrderId          int64
	SubscribeId      int64
	Subscribe        *subscribe.Subscribe
	NodeGroupId      int64
	GroupLocked      *bool
	StartTime        time.Time
	ExpireTime       time.Time
	FinishedAt       *time.Time
	Traffic          int64
	TrafficUnlimited bool
	Download         int64
	Upload           int64
	Token            string
	UUID             string
	Status           uint8
	Note             string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type SubscribeLogFilterParams struct {
	IP              string
	UserAgent       string
	UserId          int64
	Token           string
	UserSubscribeId int64
}

type LoginLogFilterParams struct {
	IP        string
	UserId    int64
	UserAgent string
	Success   *bool
}

type UserFilterParams struct {
	Search          string
	UserId          *int64
	SubscribeId     *int64
	UserSubscribeId *int64
	ShortCode       string
	Order           string // Order by id, e.g., "desc"
	Unscoped        bool   // Whether to include soft-deleted records
}

type EmailRecipientFilter struct {
	Scope             int8
	RegisterStartTime int64
	RegisterEndTime   int64
}

type SubscribeFilter struct {
	Subscribers []int64
	IsActive    *bool
	StartTime   int64
	EndTime     int64
}

type customUserLogicModel interface {
	QueryPageList(ctx context.Context, page, size int, filter *UserFilterParams) ([]*User, int64, error)
	FindUsersByIds(ctx context.Context, ids []int64) ([]*User, error)
	CountAffiliates(ctx context.Context, refererId int64) (int64, error)
	QueryAffiliateList(ctx context.Context, refererId int64, page, size int) ([]*User, int64, error)
	FindOneByReferCode(ctx context.Context, referCode string) (*User, error)
	BatchDeleteUser(ctx context.Context, ids []int64) error
	InsertSubscribe(ctx context.Context, data *Subscribe) error
	FindOneSubscribeByToken(ctx context.Context, token string) (*Subscribe, error)
	FindOneSubscribeByOrderId(ctx context.Context, orderId int64) (*Subscribe, error)
	FindOneSubscribe(ctx context.Context, id int64) (*Subscribe, error)
	FindSubscribesByIds(ctx context.Context, ids []int64) ([]*Subscribe, error)
	QueryMonthlyResetSubscribeIds(ctx context.Context, subscribeIds []int64, now time.Time) ([]int64, error)
	QueryFirstResetSubscribeIds(ctx context.Context, subscribeIds []int64, now time.Time) ([]int64, error)
	QueryYearlyResetSubscribeIds(ctx context.Context, subscribeIds []int64, now time.Time) ([]int64, error)
	ResetSubscribeTrafficByIds(ctx context.Context, ids []int64) error
	FindTrafficExceededSubscribes(ctx context.Context) ([]*Subscribe, error)
	FindExpiredSubscribes(ctx context.Context, now time.Time) ([]*Subscribe, error)
	MarkSubscribesFinished(ctx context.Context, ids []int64, status uint8, finishedAt time.Time) error
	UpdateSubscribe(ctx context.Context, data *Subscribe) error
	DeleteSubscribe(ctx context.Context, token string) error
	DeleteSubscribeById(ctx context.Context, id int64) error
	QueryUserSubscribe(ctx context.Context, userId int64, status ...int64) ([]*SubscribeDetails, error)
	FindOneSubscribeDetailsById(ctx context.Context, id int64) (*SubscribeDetails, error)
	FindOneUserSubscribe(ctx context.Context, id int64) (*SubscribeDetails, error)
	FindUsersSubscribeBySubscribeId(ctx context.Context, subscribeId int64) ([]*Subscribe, error)
	FindUserSubscribesByStatus(ctx context.Context, status ...int64) ([]*Subscribe, error)
	ActivatePendingSubscribesBySubscribeId(ctx context.Context, subscribeId int64) error
	CountUserSubscribesByUserAndSubscribe(ctx context.Context, userId, subscribeId int64) (int64, error)
	CountUserSubscribesBySubscribeIdAndStatus(ctx context.Context, subscribeId int64, status ...int64) (int64, error)
	UpdateUserSubscribeWithTraffic(ctx context.Context, id, download, upload int64, isExpired bool) error
	QueryResisterUserTotalByDate(ctx context.Context, date time.Time) (int64, error)
	QueryResisterUserTotalByMonthly(ctx context.Context, date time.Time) (int64, error)
	QueryResisterUserTotal(ctx context.Context) (int64, error)
	CountEnabledUsers(ctx context.Context) (int64, error)
	QueryAdminUsers(ctx context.Context) ([]*User, error)
	UpdateUserCache(ctx context.Context, data *User) error
	UpdateUserSubscribeCache(ctx context.Context, data *Subscribe) error
	QueryActiveSubscriptions(ctx context.Context, subscribeId ...int64) (map[int64]int64, error)
	FindUserAuthMethods(ctx context.Context, userId int64) ([]*AuthMethods, error)
	InsertUserAuthMethods(ctx context.Context, data *AuthMethods) error
	UpdateUserAuthMethods(ctx context.Context, data *AuthMethods) error
	DeleteUserAuthMethods(ctx context.Context, userId int64, platform string) error
	UpdateUserAuthMethodOwner(ctx context.Context, authType, identifier string, userId int64) error
	DeleteUserAuthMethodByIdentifier(ctx context.Context, authType, identifier string) error
	UpsertUserAuthMethod(ctx context.Context, data *AuthMethods) error
	FindUserAuthMethodByOpenID(ctx context.Context, method, openID string) (*AuthMethods, error)
	FindUserAuthMethodByUserId(ctx context.Context, method string, userId int64) (*AuthMethods, error)
	FindUserAuthMethodByPlatform(ctx context.Context, userId int64, platform string) (*AuthMethods, error)
	QueryEmailRecipients(ctx context.Context, filter *EmailRecipientFilter) ([]string, error)
	CountEmailRecipients(ctx context.Context, filter *EmailRecipientFilter) (int64, error)
	FindOneByEmail(ctx context.Context, email string) (*User, error)
	FindOneDevice(ctx context.Context, id int64) (*Device, error)
	QueryDeviceList(ctx context.Context, userid int64) ([]*Device, int64, error)
	QueryDevicePageList(ctx context.Context, userid, subscribeId int64, page, size int) ([]*Device, int64, error)
	UpdateDevice(ctx context.Context, data *Device) error
	FindOneDeviceByIdentifier(ctx context.Context, id string) (*Device, error)
	DeleteDevice(ctx context.Context, id int64) error
	InsertDevice(ctx context.Context, data *Device) error
	FindDeviceOnlineRecord(ctx context.Context, userId int64, startTime, endTime string) (*DeviceOnlineRecord, error)
	InsertDeviceOnlineRecord(ctx context.Context, data *DeviceOnlineRecord) error
	InsertWithdrawal(ctx context.Context, data *Withdrawal) error

	QuerySubscribeIdsByFilter(ctx context.Context, filter *SubscribeFilter) ([]int64, error)
	CountSubscribesByFilter(ctx context.Context, filter *SubscribeFilter) (int64, error)

	ClearSubscribeCache(ctx context.Context, data ...*Subscribe) error
	ClearUserCache(ctx context.Context, data ...*User) error

	QueryDailyUserStatisticsList(ctx context.Context, date time.Time) ([]UserStatisticsWithDate, error)
	QueryMonthlyUserStatisticsList(ctx context.Context, date time.Time) ([]UserStatisticsWithDate, error)
}

type UserStatisticsWithDate struct {
	Date              string
	Register          int64
	NewOrderUsers     int64
	RenewalOrderUsers int64
}

// NewModel returns a model for the database table.
func NewModel(conn *ent.Client, c *redis.Client) Model {
	return &customUserModel{
		defaultUserModel: newUserModel(conn, c),
	}
}

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

func applySubscribeFilter(query *ent.UserSubscribeQuery, filter *SubscribeFilter) *ent.UserSubscribeQuery {
	if filter == nil {
		return query
	}
	if len(filter.Subscribers) > 0 {
		query = query.Where(entsub.SubscribeIDIn(filter.Subscribers...))
	}
	if filter.IsActive != nil && *filter.IsActive {
		query = query.Where(entsub.StatusIn(0, 1, 2))
	}
	if filter.StartTime != 0 {
		query = query.Where(entsub.StartTimeLTE(time.UnixMilli(filter.StartTime)))
	}
	if filter.EndTime != 0 {
		query = query.Where(entsub.ExpireTimeGTE(time.UnixMilli(filter.EndTime)))
	}
	return query
}

func (m *customUserModel) QuerySubscribeIdsByFilter(ctx context.Context, filter *SubscribeFilter) ([]int64, error) {
	var ids []int64
	err := applySubscribeFilter(m.db.UserSubscribe.Query(), filter).Select(entsub.FieldID).Scan(ctx, &ids)
	return ids, err
}

func (m *customUserModel) CountSubscribesByFilter(ctx context.Context, filter *SubscribeFilter) (int64, error) {
	count, err := applySubscribeFilter(m.db.UserSubscribe.Query(), filter).Count(ctx)
	return int64(count), err
}

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

func (m *customUserModel) QueryResisterUserTotalByDate(ctx context.Context, date time.Time) (int64, error) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	end := start.AddDate(0, 0, 1)
	count, err := m.db.User.Query().Where(entuser.CreatedAtGTE(start), entuser.CreatedAtLT(end), entuser.DeletedAtIsNil()).Count(ctx)
	return int64(count), err
}

func (m *customUserModel) QueryResisterUserTotalByMonthly(ctx context.Context, date time.Time) (int64, error) {
	start := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	end := start.AddDate(0, 1, 0)
	count, err := m.db.User.Query().Where(entuser.CreatedAtGTE(start), entuser.CreatedAtLT(end), entuser.DeletedAtIsNil()).Count(ctx)
	return int64(count), err
}

func (m *customUserModel) QueryResisterUserTotal(ctx context.Context) (int64, error) {
	count, err := m.db.User.Query().Where(entuser.DeletedAtIsNil()).Count(ctx)
	return int64(count), err
}

func (m *customUserModel) CountEnabledUsers(ctx context.Context) (int64, error) {
	count, err := m.db.User.Query().Where(entuser.Enable(true), entuser.DeletedAtIsNil()).Count(ctx)
	return int64(count), err
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
	return entToSubscribeDetails(item), err
}

// QueryDailyUserStatisticsList Query daily user statistics list for the current month (from 1st to current date)
func (m *customUserModel) QueryDailyUserStatisticsList(ctx context.Context, date time.Time) ([]UserStatisticsWithDate, error) {
	firstDay := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	return m.queryUserStatistics(ctx, firstDay, date, "day")
}

// QueryMonthlyUserStatisticsList Query monthly user statistics list for the past 6 months
func (m *customUserModel) QueryMonthlyUserStatisticsList(ctx context.Context, date time.Time) ([]UserStatisticsWithDate, error) {
	sixMonthsAgo := date.AddDate(0, -5, 0)
	return m.queryUserStatistics(ctx, sixMonthsAgo, date, "month")
}

func (m *customUserModel) queryUserStatistics(ctx context.Context, start, end time.Time, bucket string) ([]UserStatisticsWithDate, error) {
	d := m.db.Driver().Dialect()
	userDateExpr := userDateBucketExpr(d, "u."+entuser.FieldCreatedAt, bucket)
	orderDateExpr := userDateBucketExpr(d, "o."+entorder.FieldCreatedAt, bucket)
	query := fmt.Sprintf(`
		SELECT %s AS date,
		       COUNT(*) AS register,
		       COALESCE(MAX(n.new_order_users), 0) AS new_order_users,
		       COALESCE(MAX(r.renewal_order_users), 0) AS renewal_order_users
		FROM %s u
		LEFT JOIN (
			SELECT %s AS date, COUNT(DISTINCT %s) AS new_order_users
			FROM %s o
			WHERE %s = ? AND %s BETWEEN ? AND ? AND %s IN (?, ?)
			GROUP BY %s
		) n ON %s = n.date
		LEFT JOIN (
			SELECT %s AS date, COUNT(DISTINCT %s) AS renewal_order_users
			FROM %s o
			WHERE %s = ? AND %s BETWEEN ? AND ? AND %s IN (?, ?)
			GROUP BY %s
		) r ON %s = r.date
		WHERE u.%s BETWEEN ? AND ? AND u.%s IS NULL
		GROUP BY %s
		ORDER BY date ASC`,
		userDateExpr,
		entuser.Table,
		orderDateExpr, entorder.FieldUserID, entorder.Table, entorder.FieldIsNew, entorder.FieldCreatedAt, entorder.FieldStatus, orderDateExpr, userDateExpr,
		orderDateExpr, entorder.FieldUserID, entorder.Table, entorder.FieldIsNew, entorder.FieldCreatedAt, entorder.FieldStatus, orderDateExpr, userDateExpr,
		entuser.FieldCreatedAt, entuser.FieldDeletedAt, userDateExpr,
	)
	rows := &entsql.Rows{}
	if err := m.db.Driver().Query(ctx, query, []any{true, start, end, 2, 5, false, start, end, 2, 5, start, end}, rows); err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []UserStatisticsWithDate
	for rows.Next() {
		var item UserStatisticsWithDate
		if err := rows.Scan(&item.Date, &item.Register, &item.NewOrderUsers, &item.RenewalOrderUsers); err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

func userDateBucketExpr(dialectName, column, bucket string) string {
	if dialectName == "postgres" {
		if bucket == "month" {
			return fmt.Sprintf("TO_CHAR(%s, 'YYYY-MM')", column)
		}
		return fmt.Sprintf("TO_CHAR(%s, 'YYYY-MM-DD')", column)
	}
	if bucket == "month" {
		return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m')", column)
	}
	return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m-%%d')", column)
}
