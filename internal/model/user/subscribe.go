package user

import (
	"context"
	"fmt"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/predicate"
	entsub "github.com/perfect-panel/server/ent/usersubscribe"
)

func (m *defaultUserModel) UpdateUserSubscribeCache(ctx context.Context, data *Subscribe) error {
	return m.ClearSubscribeCacheByModels(ctx, data)
}

// QueryActiveSubscriptions returns the number of active subscriptions.
func (m *defaultUserModel) QueryActiveSubscriptions(ctx context.Context, subscribeId ...int64) (map[int64]int64, error) {
	type SubscriptionCount struct {
		SubscribeId int64 `json:"subscribe_id"`
		Total       int64 `json:"total"`
	}
	var result []SubscriptionCount
	err := m.db.UserSubscribe.Query().Where(entsub.SubscribeIDIn(subscribeId...), entsub.StatusIn(1, 0)).GroupBy(entsub.FieldSubscribeID).Aggregate(ent.As(ent.Count(), "total")).Scan(ctx, &result)

	if err != nil {
		return nil, err
	}

	resultMap := make(map[int64]int64)
	for _, item := range result {
		resultMap[item.SubscribeId] = item.Total
	}

	return resultMap, nil
}

func (m *defaultUserModel) FindOneSubscribeByOrderId(ctx context.Context, orderId int64) (*Subscribe, error) {
	item, err := m.db.UserSubscribe.Query().Where(entsub.OrderID(orderId)).First(ctx)
	return entToSubscribe(item), err
}

func (m *defaultUserModel) FindOneSubscribe(ctx context.Context, id int64) (*Subscribe, error) {
	key := fmt.Sprintf("%s%d", cacheUserSubscribeIdPrefix, id)
	var data Subscribe
	if err := getJSONCache(ctx, m.redis, key, &data); err == nil {
		return &data, nil
	}
	item, err := m.db.UserSubscribe.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	data = *entToSubscribe(item)
	_ = setJSONCache(ctx, m.redis, key, &data)
	return &data, nil
}

func (m *defaultUserModel) FindUsersSubscribeBySubscribeId(ctx context.Context, subscribeId int64) ([]*Subscribe, error) {
	items, err := m.db.UserSubscribe.Query().Where(entsub.SubscribeID(subscribeId), entsub.StatusIn(1, 0)).All(ctx)
	return entSubscribesToModels(items), err
}

func (m *defaultUserModel) FindUserSubscribesByStatus(ctx context.Context, status ...int64) ([]*Subscribe, error) {
	q := m.db.UserSubscribe.Query()
	if len(status) > 0 {
		q = q.Where(entsub.StatusIn(int64ToUint8(status)...))
	}
	items, err := q.All(ctx)
	return entSubscribesToModels(items), err
}

func (m *defaultUserModel) ActivatePendingSubscribesBySubscribeId(ctx context.Context, subscribeId int64) error {
	items, err := m.db.UserSubscribe.Query().Where(entsub.SubscribeID(subscribeId), entsub.Status(0)).All(ctx)
	pending := entSubscribesToModels(items)
	if err != nil || len(pending) == 0 {
		return err
	}

	cacheKeys := make([]string, 0)
	for _, sub := range pending {
		cacheKeys = append(cacheKeys, sub.GetCacheKeys()...)
	}

	if _, err := m.db.UserSubscribe.Update().Where(entsub.SubscribeID(subscribeId), entsub.Status(0)).SetStatus(1).Save(ctx); err != nil {
		return err
	}
	return m.GetCacheManager().ClearCache(ctx, cacheKeys...)
}

func (m *defaultUserModel) CountUserSubscribesBySubscribeIdAndStatus(ctx context.Context, subscribeId int64, status ...int64) (int64, error) {
	q := m.db.UserSubscribe.Query().Where(entsub.SubscribeID(subscribeId))
	if len(status) > 0 {
		q = q.Where(entsub.StatusIn(int64ToUint8(status)...))
	}
	count, err := q.Count(ctx)
	return int64(count), err
}

func (m *defaultUserModel) CountUserSubscribesByUserAndSubscribe(ctx context.Context, userId, subscribeId int64) (int64, error) {
	count, err := m.db.UserSubscribe.Query().Where(entsub.UserID(userId), entsub.SubscribeID(subscribeId)).Count(ctx)
	return int64(count), err
}

// QueryUserSubscribe returns a list of records that meet the conditions.
func (m *defaultUserModel) QueryUserSubscribe(ctx context.Context, userId int64, status ...int64) ([]*SubscribeDetails, error) {
	key := fmt.Sprintf("%s%d", cacheUserSubscribeUserPrefix, userId)
	var list []*SubscribeDetails
	if err := getJSONCache(ctx, m.redis, key, &list); err == nil {
		return list, nil
	}
	now := time.Now()
	sevenDaysAgo := now.Add(-7 * 24 * time.Hour)
	q := m.db.UserSubscribe.Query().Where(entsub.UserID(userId), predicate.UserSubscribe(func(s *entsql.Selector) {
		s.Where(entsql.Or(entsql.GT(s.C(entsub.FieldExpireTime), now), entsql.GTE(s.C(entsub.FieldFinishedAt), sevenDaysAgo), entsql.EQ(s.C(entsub.FieldExpireTime), time.UnixMilli(0))))
	}))
	if len(status) > 0 {
		q = q.Where(entsub.StatusIn(int64ToUint8(status)...))
	}
	items, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	list = entSubscribeDetailsToModels(items)
	_ = setJSONCache(ctx, m.redis, key, list)
	return list, nil
}

// FindOneUserSubscribe  finds a subscribeDetails by id.
func (m *defaultUserModel) FindOneUserSubscribe(ctx context.Context, id int64) (subscribeDetails *SubscribeDetails, err error) {
	//TODO cache
	//key := fmt.Sprintf("%s%d", cacheUserSubscribeUserPrefix, userId)
	item, err := m.db.UserSubscribe.Get(ctx, id)
	return entToSubscribeDetails(item), err
}

// FindOneSubscribeByToken  finds a record by token.
func (m *defaultUserModel) FindOneSubscribeByToken(ctx context.Context, token string) (*Subscribe, error) {
	key := fmt.Sprintf("%s%s", cacheUserSubscribeTokenPrefix, token)
	var data Subscribe
	if err := getJSONCache(ctx, m.redis, key, &data); err == nil {
		return &data, nil
	}
	item, err := m.db.UserSubscribe.Query().Where(entsub.Token(token)).First(ctx)
	if err != nil {
		return nil, err
	}
	data = *entToSubscribe(item)
	_ = setJSONCache(ctx, m.redis, key, &data)
	return &data, nil
}

// UpdateSubscribe updates a record.
func (m *defaultUserModel) UpdateSubscribe(ctx context.Context, data *Subscribe) error {
	old, err := m.FindOneSubscribe(ctx, data.Id)
	if err != nil {
		return err
	}

	if data.GroupLocked == nil {
		if old.GroupLocked != nil {
			data.GroupLocked = old.GroupLocked
		} else {
			groupLocked := false
			data.GroupLocked = &groupLocked
		}
	}

	// 使用 defer 确保更新后清理缓存
	defer func() {
		if clearErr := m.ClearSubscribeCacheByModels(ctx, old, data); clearErr != nil {
			// 记录清理缓存错误
		}
	}()

	updated, err := subscribeUpdate(m.db.UserSubscribe.UpdateOneID(data.Id), data).Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToSubscribe(updated)
	return nil
}

// DeleteSubscribe deletes a record.
func (m *defaultUserModel) DeleteSubscribe(ctx context.Context, token string) error {
	data, err := m.FindOneSubscribeByToken(ctx, token)
	if err != nil {
		return err
	}

	// 使用 defer 确保删除后清理缓存
	defer func() {
		if clearErr := m.ClearSubscribeCacheByModels(ctx, data); clearErr != nil {
			// 记录清理缓存错误
		}
	}()

	_, err = m.db.UserSubscribe.Delete().Where(entsub.Token(token)).Exec(ctx)
	return err
}

// InsertSubscribe insert Subscribe into the database.
func (m *defaultUserModel) InsertSubscribe(ctx context.Context, data *Subscribe) error {
	// 使用 defer 确保插入后清理相关缓存
	defer func() {
		if clearErr := m.ClearSubscribeCacheByModels(ctx, data); clearErr != nil {
			// 记录清理缓存错误
		}
	}()

	created, err := subscribeCreate(m.db.UserSubscribe.Create(), data).Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToSubscribe(created)
	return nil
}

func (m *defaultUserModel) DeleteSubscribeById(ctx context.Context, id int64) error {
	data, err := m.FindOneSubscribe(ctx, id)
	if err != nil {
		return err
	}

	// 使用 defer 确保删除后清理缓存
	defer func() {
		if clearErr := m.ClearSubscribeCacheByModels(ctx, data); clearErr != nil {
			// 记录清理缓存错误
		}
	}()

	return m.db.UserSubscribe.DeleteOneID(id).Exec(ctx)
}

func (m *defaultUserModel) ClearSubscribeCache(ctx context.Context, data ...*Subscribe) error {
	return m.ClearSubscribeCacheByModels(ctx, data...)
}
