package subscribe

import (
	"context"
	"fmt"
	"strings"

	"entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	entnode "github.com/perfect-panel/server/ent/node"
	"github.com/perfect-panel/server/ent/predicate"
	entsubscribe "github.com/perfect-panel/server/ent/subscribe"
	entusersubscribe "github.com/perfect-panel/server/ent/usersubscribe"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/redis/go-redis/v9"
)

var _ Model = (*customSubscribeModel)(nil)
var (
	cacheSubscribeIdPrefix       = "cache:subscribe:id:"
	userSubscribeUserCachePrefix = "cache:user:subscribe:user:"
)

type (
	Model interface {
		subscribeModel
		customSubscribeLogicModel
	}
	subscribeModel interface {
		Insert(ctx context.Context, data *Subscribe) error
		FindOne(ctx context.Context, id int64) (*Subscribe, error)
		Update(ctx context.Context, data *Subscribe) error
		Delete(ctx context.Context, id int64) error
	}

	customSubscribeModel struct {
		*defaultSubscribeModel
	}
	defaultSubscribeModel struct {
		db    *ent.Client
		redis *redis.Client
		table string
	}
)

func newSubscribeModel(db *ent.Client, c *redis.Client) *defaultSubscribeModel {
	return &defaultSubscribeModel{db: db, redis: c, table: "subscribe"}
}

func (m *defaultSubscribeModel) batchGetCacheKeys(subscribes ...*Subscribe) []string {
	var keys []string
	for _, item := range subscribes {
		keys = append(keys, m.getCacheKeys(item)...)
	}
	return keys
}

func (m *defaultSubscribeModel) getCacheKeys(data *Subscribe) []string {
	if data == nil {
		return []string{}
	}
	var keys []string
	if data.Nodes != "" {
		ids := tool.StringSliceToInt64Slice(strings.Split(data.Nodes, ","))
		nodes, err := m.db.Node.Query().Where(entnode.IDIn(ids...)).All(context.Background())
		if err == nil {
			keys = append(keys, serverUserListKeys(nodes)...)
		}
	}
	if data.NodeTags != "" {
		tags := tool.RemoveDuplicateElements(strings.Split(data.NodeTags, ",")...)
		nodes, err := m.db.Node.Query().Where(nodeTagsContainAny(tags)).All(context.Background())
		if err == nil {
			keys = append(keys, serverUserListKeys(nodes)...)
		}
	}
	return append(keys, fmt.Sprintf("%s%v", cacheSubscribeIdPrefix, data.Id))
}

func (m *defaultSubscribeModel) getUserSubscribeCacheKeys(ctx context.Context, subscribeId int64) ([]string, error) {
	var userIds []int64
	if err := m.db.UserSubscribe.Query().Where(entusersubscribe.SubscribeID(subscribeId), entusersubscribe.StatusIn(0, 1)).Unique(true).Select(entusersubscribe.FieldUserID).Scan(ctx, &userIds); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(userIds))
	for _, userId := range userIds {
		keys = append(keys, fmt.Sprintf("%s%d", userSubscribeUserCachePrefix, userId))
	}
	return keys, nil
}

func (m *defaultSubscribeModel) Insert(ctx context.Context, data *Subscribe) error {
	if data.Sort == 0 {
		maxSort, err := m.db.Subscribe.Query().Aggregate(ent.Max(entsubscribe.FieldSort)).Int(ctx)
		if err != nil {
			return err
		}
		data.Sort = int64(maxSort) + 1
	}
	created, err := m.subscribeCreate(data).Save(ctx)
	if err != nil {
		return err
	}
	data.Id = created.ID
	data.CreatedAt = created.CreatedAt
	data.UpdatedAt = created.UpdatedAt
	return m.delCache(ctx, m.getCacheKeys(data)...)
}

func (m *defaultSubscribeModel) FindOne(ctx context.Context, id int64) (*Subscribe, error) {
	data, err := m.db.Subscribe.Query().Where(entsubscribe.ID(id)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return entToSubscribe(data), nil
}

func (m *defaultSubscribeModel) Update(ctx context.Context, data *Subscribe) error {
	old, err := m.FindOne(ctx, data.Id)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	cacheKeys := m.getCacheKeys(old)
	userSubscribeCacheKeys, err := m.getUserSubscribeCacheKeys(ctx, data.Id)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	cacheKeys = append(cacheKeys, userSubscribeCacheKeys...)
	if err := m.ensureUniqueSort(ctx, data); err != nil {
		return err
	}
	if err := m.subscribeUpdate(data).Exec(ctx); err != nil {
		return err
	}
	return m.delCache(ctx, cacheKeys...)
}

func (m *defaultSubscribeModel) Delete(ctx context.Context, id int64) error {
	data, err := m.FindOne(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return err
	}
	cacheKeys := m.getCacheKeys(data)
	userSubscribeCacheKeys, err := m.getUserSubscribeCacheKeys(ctx, id)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	cacheKeys = append(cacheKeys, userSubscribeCacheKeys...)
	if err := m.db.Subscribe.DeleteOneID(id).Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return err
	}
	if _, err := m.db.Subscribe.Update().Where(entsubscribe.SortGT(data.Sort)).AddSort(-1).Save(ctx); err != nil {
		return err
	}
	return m.delCache(ctx, cacheKeys...)
}

func (m *defaultSubscribeModel) subscribeCreate(data *Subscribe) *ent.SubscribeCreate {
	return m.db.Subscribe.Create().SetName(data.Name).SetLanguage(data.Language).SetDescription(data.Description).SetUnitPrice(data.UnitPrice).SetUnitTime(data.UnitTime).SetDiscount(data.Discount).SetReplacement(data.Replacement).SetInventory(data.Inventory).SetTraffic(data.Traffic).SetTrafficUnlimited(data.TrafficUnlimited).SetSpeedLimit(data.SpeedLimit).SetDeviceLimit(data.DeviceLimit).SetQuota(data.Quota).SetNodes(data.Nodes).SetNodeTags(data.NodeTags).SetNodeGroupIds([]int64(data.NodeGroupIds)).SetNodeGroupID(data.NodeGroupId).SetTrafficLimit(data.TrafficLimit).SetNillableShow(data.Show).SetNillableSell(data.Sell).SetSort(data.Sort).SetDeductionRatio(data.DeductionRatio).SetNillableAllowDeduction(data.AllowDeduction).SetResetCycle(data.ResetCycle).SetNillableRenewalReset(data.RenewalReset).SetShowOriginalPrice(data.ShowOriginalPrice)
}

func (m *defaultSubscribeModel) subscribeUpdate(data *Subscribe) *ent.SubscribeUpdateOne {
	return m.db.Subscribe.UpdateOneID(data.Id).SetName(data.Name).SetLanguage(data.Language).SetDescription(data.Description).SetUnitPrice(data.UnitPrice).SetUnitTime(data.UnitTime).SetDiscount(data.Discount).SetReplacement(data.Replacement).SetInventory(data.Inventory).SetTraffic(data.Traffic).SetTrafficUnlimited(data.TrafficUnlimited).SetSpeedLimit(data.SpeedLimit).SetDeviceLimit(data.DeviceLimit).SetQuota(data.Quota).SetNodes(data.Nodes).SetNodeTags(data.NodeTags).SetNodeGroupIds([]int64(data.NodeGroupIds)).SetNodeGroupID(data.NodeGroupId).SetTrafficLimit(data.TrafficLimit).SetNillableShow(data.Show).SetNillableSell(data.Sell).SetSort(data.Sort).SetDeductionRatio(data.DeductionRatio).SetNillableAllowDeduction(data.AllowDeduction).SetResetCycle(data.ResetCycle).SetNillableRenewalReset(data.RenewalReset).SetShowOriginalPrice(data.ShowOriginalPrice)
}

func (m *defaultSubscribeModel) ensureUniqueSort(ctx context.Context, data *Subscribe) error {
	count, err := m.db.Subscribe.Query().Where(entsubscribe.Sort(data.Sort), entsubscribe.IDNEQ(data.Id)).Count(ctx)
	if err != nil || count == 0 {
		return err
	}
	maxSort, err := m.db.Subscribe.Query().Aggregate(ent.Max(entsubscribe.FieldSort)).Int(ctx)
	if err != nil {
		return err
	}
	data.Sort = int64(maxSort) + 1
	return nil
}

func (m *defaultSubscribeModel) delCache(ctx context.Context, keys ...string) error {
	if len(keys) == 0 || m.redis == nil {
		return nil
	}
	return m.redis.Del(ctx, tool.RemoveDuplicateElements(keys...)...).Err()
}

func serverUserListKeys(nodes []*ent.Node) []string {
	keys := make([]string, 0, len(nodes)*3)
	for _, n := range nodes {
		keys = append(keys, fmt.Sprintf("%s%d", node.ServerUserListCacheKey, n.ServerID))
		keys = append(keys, fmt.Sprintf("%s%d:%s", node.ServerUserListCacheKey, n.ServerID, n.Protocol))
		keys = append(keys, fmt.Sprintf("%s%d:%s:%s", node.ServerUserListCacheKey, n.ServerID, n.Protocol, n.ProtocolID))
	}
	return keys
}

func nodeTagsContainAny(tags []string) predicate.Node {
	predicates := make([]predicate.Node, 0, len(tags))
	for _, tag := range tags {
		value := tag
		predicates = append(predicates, predicate.Node(func(s *sql.Selector) {
			s.Where(sql.ExprP("FIND_IN_SET(?, tags)", value))
		}))
	}
	return entnode.Or(predicates...)
}
