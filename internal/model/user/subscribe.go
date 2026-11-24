package user

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect"
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

// FindOneSubscribeForUpdate reads a subscription under a row lock so a
// concurrent writer cannot interleave with the caller's read-modify-write.
//
// SQLite has no `SELECT ... FOR UPDATE`, so the clause is omitted there (the
// dialect the tests run on); MySQL and PostgreSQL, the supported production
// engines, always take the lock.
func (m *defaultUserModel) FindOneSubscribeForUpdate(ctx context.Context, id int64) (*Subscribe, error) {
	query := m.db.UserSubscribe.Query().Where(entsub.ID(id))
	if m.db.Driver().Dialect() != dialect.SQLite {
		query = query.Where(func(selector *entsql.Selector) { selector.ForUpdate() })
	}
	item, err := query.Only(ctx)
	return entToSubscribe(item), err
}

func (m *defaultUserModel) CancelSubscribe(ctx context.Context, id, userID int64) (bool, error) {
	count, err := m.db.UserSubscribe.Update().Where(entsub.ID(id), entsub.UserID(userID), entsub.StatusIn(0, 1, 2), entsub.EntitlementSourceEQ("")).SetStatus(4).SetFinishedAt(time.Now()).Save(ctx)
	return count == 1, err
}

func (m *defaultUserModel) RenewSubscribe(ctx context.Context, data *Subscribe, resetTraffic bool) error {
	update := m.db.UserSubscribe.Update().Where(entsub.ID(data.Id), entsub.UserID(data.UserId), entsub.SubscribeID(data.SubscribeId), entsub.StatusLT(4), entsub.EntitlementSourceEQ("")).
		SetExpireTime(data.ExpireTime).SetStatus(1).ClearFinishedAt().SetTrafficUnlimited(data.TrafficUnlimited).
		SetExpiredDownload(0).SetExpiredUpload(0)
	if resetTraffic {
		update.SetDownload(0).SetUpload(0)
	}
	count, err := update.Save(ctx)
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("subscription changed during renewal")
	}
	return nil
}

func (m *defaultUserModel) ResetSubscribeTraffic(ctx context.Context, id int64) error {
	count, err := m.db.UserSubscribe.Update().Where(entsub.ID(id), entsub.StatusLT(4), entsub.EntitlementSourceEQ("")).
		SetDownload(0).SetUpload(0).SetExpiredDownload(0).SetExpiredUpload(0).SetStatus(1).ClearFinishedAt().Save(ctx)
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("subscription changed during traffic reset")
	}
	return nil
}

// QuotaGrant describes the columns a quota task is allowed to change on one
// subscription. It is deliberately a set of optional fields rather than a whole
// Subscribe row: the quota task loads its subscribers before it opens the
// transaction, so writing a snapshot back would revert any traffic settlement or
// credential rotation that landed in between.
type QuotaGrant struct {
	// ExpireTime is the new expiry, when the grant extends the subscription.
	ExpireTime *time.Time
	// Status is the new status, when the grant also changes it.
	Status *uint8
	// ResetTraffic zeroes the counters when the grant resets traffic.
	ResetTraffic bool
}

// Empty reports whether the grant would change nothing.
func (g QuotaGrant) Empty() bool {
	return g.ExpireTime == nil && g.Status == nil && !g.ResetTraffic
}

// ApplyQuotaGrant persists exactly the columns the grant names, leaving every
// other column — traffic counters, credentials, node group — as the row has
// them now.
func (m *defaultUserModel) ApplyQuotaGrant(ctx context.Context, id int64, grant QuotaGrant) error {
	if grant.Empty() {
		return nil
	}
	update := m.db.UserSubscribe.Update().Where(entsub.ID(id), entsub.StatusLT(4), entsub.EntitlementSourceEQ(""))
	if grant.ExpireTime != nil {
		update.SetExpireTime(*grant.ExpireTime)
	}
	if grant.Status != nil {
		update.SetStatus(*grant.Status)
	}
	if grant.ResetTraffic {
		update.SetDownload(0).SetUpload(0)
	}
	count, err := update.Save(ctx)
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("subscription changed during quota grant")
	}
	return nil
}

func (m *defaultUserModel) RotateSubscribeCredentials(ctx context.Context, id, userID int64, token, nodeUUID string) error {
	if token == "" || nodeUUID == "" {
		return fmt.Errorf("subscription credentials cannot be empty")
	}
	count, err := m.db.UserSubscribe.Update().Where(entsub.ID(id), entsub.UserID(userID)).SetToken(token).SetUUID(nodeUUID).Save(ctx)
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("subscription does not belong to account")
	}
	return nil
}

func (m *defaultUserModel) UpdateSubscribeNote(ctx context.Context, id, userID int64, note string) error {
	count, err := m.db.UserSubscribe.Update().Where(entsub.ID(id), entsub.UserID(userID)).SetNote(note).Save(ctx)
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("subscription does not belong to account")
	}
	return nil
}

func (m *defaultUserModel) CompareSubscribeStatus(ctx context.Context, id int64, previous, next uint8) (bool, error) {
	count, err := m.db.UserSubscribe.Update().Where(entsub.ID(id), entsub.Status(previous), entsub.EntitlementSourceEQ("")).SetStatus(next).Save(ctx)
	return count == 1, err
}

func (m *defaultUserModel) ResetSubscribeCounters(ctx context.Context, id int64) error {
	subscription, err := m.FindOneSubscribe(ctx, id)
	if err != nil {
		return err
	}
	if subscription.EntitlementSource != "" {
		return ErrProviderManaged
	}
	return m.db.UserSubscribe.UpdateOneID(id).SetDownload(0).SetUpload(0).Exec(ctx)
}

func (m *defaultUserModel) UpdateSubscribeSettings(ctx context.Context, data *Subscribe, previous uint8) error {
	update := m.db.UserSubscribe.Update().Where(entsub.ID(data.Id), entsub.Status(previous), entsub.EntitlementSourceEQ("")).
		SetSubscribeID(data.SubscribeId).SetExpireTime(data.ExpireTime).SetTraffic(data.Traffic).
		SetTrafficUnlimited(data.TrafficUnlimited).SetDownload(data.Download).SetUpload(data.Upload).SetStatus(data.Status)
	if data.Status == 1 {
		update.ClearFinishedAt()
	} else {
		update.SetFinishedAt(time.Now())
	}
	count, err := update.Save(ctx)
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("subscription changed; refresh and retry")
	}
	return nil
}

func (m *defaultUserModel) RedeemSubscribe(ctx context.Context, data *Subscribe, resetTraffic bool) error {
	update := m.db.UserSubscribe.Update().Where(entsub.ID(data.Id), entsub.UserID(data.UserId), entsub.StatusLT(4), entsub.EntitlementSourceEQ("")).
		SetOrderID(data.OrderId).SetExpireTime(data.ExpireTime).SetStatus(1).ClearFinishedAt().SetTrafficUnlimited(data.TrafficUnlimited)
	if resetTraffic {
		update.SetTraffic(data.Traffic).SetDownload(0).SetUpload(0).SetExpiredDownload(0).SetExpiredUpload(0)
	}
	count, err := update.Save(ctx)
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("subscription changed during redemption")
	}
	return nil
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

// FindUnlockedSubscribesForGroupUpdate reads candidates in subscription ID order inside the assignment transaction.
func (m *defaultUserModel) FindUnlockedSubscribesForGroupUpdate(ctx context.Context, status ...int64) ([]*Subscribe, error) {
	q := m.db.UserSubscribe.Query().Where(entsub.GroupLocked(false)).Order(entsub.ByID())
	if len(status) > 0 {
		q = q.Where(entsub.StatusIn(int64ToUint8(status)...))
	}
	if m.db.Driver().Dialect() != dialect.SQLite {
		q = q.Where(func(s *entsql.Selector) { s.ForUpdate() })
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

	if _, err := m.db.UserSubscribe.Update().Where(entsub.SubscribeID(subscribeId), entsub.Status(0), entsub.EntitlementSourceEQ("")).SetStatus(1).Save(ctx); err != nil {
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
	if old.EntitlementSource != "" {
		return ErrProviderManaged
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
