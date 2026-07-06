package user

import (
	"context"
	"time"

	"github.com/perfect-panel/server/ent"
	"github.com/redis/go-redis/v9"
)

const (
	cacheUserSubscribeTokenPrefix = "cache:user:subscribe:token:"
	cacheUserSubscribeUserPrefix  = "cache:user:subscribe:user:"
	cacheUserSubscribeIdPrefix    = "cache:user:subscribe:id:"
	cacheUserDeviceNumberPrefix   = "cache:user:device:number:"
	cacheUserDeviceIdPrefix       = "cache:user:device:id:"
)

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

// NewModel returns a model for the database table.
func NewModel(conn *ent.Client, c *redis.Client) Model {
	return &customUserModel{
		defaultUserModel: newUserModel(conn, c),
	}
}
