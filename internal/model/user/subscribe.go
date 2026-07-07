package user

import (
	"context"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/predicate"
	entplan "github.com/perfect-panel/server/ent/subscribe"
	entuser "github.com/perfect-panel/server/ent/user"
	entsub "github.com/perfect-panel/server/ent/usersubscribe"
)

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
	item, err := m.db.UserSubscribe.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return entToSubscribe(item), nil
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

func (m *defaultUserModel) FindUserSubscribesWithNodeGroup(ctx context.Context) ([]*Subscribe, error) {
	items, err := m.db.UserSubscribe.Query().Where(entsub.NodeGroupIDGT(0)).All(ctx)
	return entSubscribesToModels(items), err
}

func (m *defaultUserModel) FindUserSubscribesByUserAndStatus(ctx context.Context, userId int64, status ...int64) ([]*Subscribe, error) {
	q := m.db.UserSubscribe.Query().Where(entsub.UserID(userId))
	if len(status) > 0 {
		q = q.Where(entsub.StatusIn(int64ToUint8(status)...))
	}
	items, err := q.All(ctx)
	return entSubscribesToModels(items), err
}

func (m *defaultUserModel) FindUnlockedUserSubscribesByStatus(ctx context.Context, status ...int64) ([]*Subscribe, error) {
	q := m.db.UserSubscribe.Query().Where(entsub.GroupLocked(false))
	if len(status) > 0 {
		q = q.Where(entsub.StatusIn(int64ToUint8(status)...))
	}
	items, err := q.All(ctx)
	return entSubscribesToModels(items), err
}

func (m *defaultUserModel) FindUnlockedUserSubscribesStatusNotIn(ctx context.Context, status ...int64) ([]*Subscribe, error) {
	q := m.db.UserSubscribe.Query().Where(entsub.GroupLocked(false))
	if len(status) > 0 {
		q = q.Where(entsub.StatusNotIn(int64ToUint8(status)...))
	}
	items, err := q.All(ctx)
	return entSubscribesToModels(items), err
}

func (m *defaultUserModel) UpdateUserSubscribeNodeGroup(ctx context.Context, id, nodeGroupId int64) error {
	return m.db.UserSubscribe.UpdateOneID(id).SetNodeGroupID(nodeGroupId).Exec(ctx)
}

func (m *defaultUserModel) ActivatePendingSubscribesBySubscribeId(ctx context.Context, subscribeId int64) error {
	items, err := m.db.UserSubscribe.Query().Where(entsub.SubscribeID(subscribeId), entsub.Status(0)).All(ctx)
	pending := entSubscribesToModels(items)
	if err != nil || len(pending) == 0 {
		return err
	}

	if _, err := m.db.UserSubscribe.Update().Where(entsub.SubscribeID(subscribeId), entsub.Status(0)).SetStatus(1).Save(ctx); err != nil {
		return err
	}
	return nil
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
	list := entSubscribeDetailsToModels(items)
	if err := m.hydrateSubscribeDetails(ctx, list...); err != nil {
		return nil, err
	}
	return list, nil
}

// FindOneUserSubscribe  finds a subscribeDetails by id.
func (m *defaultUserModel) FindOneUserSubscribe(ctx context.Context, id int64) (subscribeDetails *SubscribeDetails, err error) {
	item, err := m.db.UserSubscribe.Get(ctx, id)
	data := entToSubscribeDetails(item)
	if err != nil || data == nil {
		return data, err
	}
	if err := m.hydrateSubscribeDetails(ctx, data); err != nil {
		return nil, err
	}
	return data, nil
}

func (m *defaultUserModel) hydrateSubscribeDetails(ctx context.Context, list ...*SubscribeDetails) error {
	if len(list) == 0 {
		return nil
	}
	userIds := make([]int64, 0, len(list))
	subscribeIds := make([]int64, 0, len(list))
	seenUsers := make(map[int64]struct{}, len(list))
	seenSubscribes := make(map[int64]struct{}, len(list))
	for _, item := range list {
		if item == nil {
			continue
		}
		if _, ok := seenUsers[item.UserId]; !ok && item.UserId > 0 {
			seenUsers[item.UserId] = struct{}{}
			userIds = append(userIds, item.UserId)
		}
		if _, ok := seenSubscribes[item.SubscribeId]; !ok && item.SubscribeId > 0 {
			seenSubscribes[item.SubscribeId] = struct{}{}
			subscribeIds = append(subscribeIds, item.SubscribeId)
		}
	}

	users := make(map[int64]*User, len(userIds))
	if len(userIds) > 0 {
		items, err := m.db.User.Query().Where(entuser.IDIn(userIds...)).All(ctx)
		if err != nil {
			return err
		}
		for _, item := range items {
			users[item.ID] = entToUser(item)
		}
	}

	subscribes := make(map[int64]*ent.Subscribe, len(subscribeIds))
	if len(subscribeIds) > 0 {
		items, err := m.db.Subscribe.Query().Where(entplan.IDIn(subscribeIds...)).All(ctx)
		if err != nil {
			return err
		}
		for _, item := range items {
			subscribes[item.ID] = item
		}
	}

	for _, item := range list {
		if item == nil {
			continue
		}
		item.User = users[item.UserId]
		item.Subscribe = entToSubscribePlan(subscribes[item.SubscribeId])
	}
	return nil
}

// FindOneSubscribeByToken  finds a record by token.
func (m *defaultUserModel) FindOneSubscribeByToken(ctx context.Context, token string) (*Subscribe, error) {
	item, err := m.db.UserSubscribe.Query().Where(entsub.Token(token)).First(ctx)
	if err != nil {
		return nil, err
	}
	return entToSubscribe(item), nil
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

	updated, err := subscribeUpdate(m.db.UserSubscribe.UpdateOneID(data.Id), data).Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToSubscribe(updated)
	return nil
}

// DeleteSubscribe deletes a record.
func (m *defaultUserModel) DeleteSubscribe(ctx context.Context, token string) error {
	_, err := m.FindOneSubscribeByToken(ctx, token)
	if err != nil {
		return err
	}

	_, err = m.db.UserSubscribe.Delete().Where(entsub.Token(token)).Exec(ctx)
	return err
}

// InsertSubscribe insert Subscribe into the database.
func (m *defaultUserModel) InsertSubscribe(ctx context.Context, data *Subscribe) error {
	created, err := subscribeCreate(m.db.UserSubscribe.Create(), data).Save(ctx)
	if err != nil {
		return err
	}
	*data = *entToSubscribe(created)
	return nil
}

func (m *defaultUserModel) DeleteSubscribeById(ctx context.Context, id int64) error {
	_, err := m.FindOneSubscribe(ctx, id)
	if err != nil {
		return err
	}

	return m.db.UserSubscribe.DeleteOneID(id).Exec(ctx)
}
