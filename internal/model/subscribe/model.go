package subscribe

import (
	"context"
	"fmt"
	"strings"

	"entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/predicate"
	entsubscribe "github.com/perfect-panel/server/ent/subscribe"
	entsubscribegroup "github.com/perfect-panel/server/ent/subscribegroup"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/redis/go-redis/v9"
)

type FilterParams struct {
	Page            int      // Page Number
	Size            int      // Page Size
	Ids             []int64  // Subscribe IDs
	Node            []int64  // Node IDs
	Tags            []string // Node Tags
	Show            bool     // Show Portal Page
	Sell            bool     // Sell
	Language        string   // Language
	DefaultLanguage bool     // Default Subscribe Language Data
	Search          string   // Search Keywords
	NodeGroupId     *int64   // Node Group ID
}

type FilterByNodeGroupsParams struct {
	Page         int     // Page Number
	Size         int     // Page Size
	NodeGroupIds []int64 // Node Group IDs (multiple)
}

func (p *FilterParams) Normalize() {
	if p.Page <= 0 {
		p.Page = 1
	}
	if p.Size <= 0 {
		p.Size = 10
	}
}

type customSubscribeLogicModel interface {
	FilterList(ctx context.Context, params *FilterParams) (int64, []*Subscribe, error)
	FilterListByNodeGroups(ctx context.Context, params *FilterByNodeGroupsParams) (int64, []*Subscribe, error)
	ClearCache(ctx context.Context, id ...int64) error
	QuerySubscribeMinSortByIds(ctx context.Context, ids []int64) (int64, error)
	QueryResetCycleSubscribeIds(ctx context.Context, resetCycle int) ([]int64, error)
	UpdateSort(ctx context.Context, data []*Subscribe) error
	QueryGroupList(ctx context.Context) (int64, []*Group, error)
	QueryAll(ctx context.Context) ([]*Subscribe, error)
	FindByIds(ctx context.Context, ids []int64) ([]*Subscribe, error)
	CountByDefaultNodeGroup(ctx context.Context, nodeGroupId int64) (int64, error)
	ClearAllNodeGroupIds(ctx context.Context) error
	CreateGroup(ctx context.Context, data *Group) error
	UpdateGroup(ctx context.Context, data *Group) error
	DeleteGroup(ctx context.Context, id int64) error
	BatchDeleteGroup(ctx context.Context, ids []int64) error
}

// NewModel returns a model for the database table.
func NewModel(conn *ent.Client, c *redis.Client) Model {
	return &customSubscribeModel{
		defaultSubscribeModel: newSubscribeModel(conn, c),
	}
}

func (m *customSubscribeModel) QueryAll(ctx context.Context) ([]*Subscribe, error) {
	items, err := m.db.Subscribe.Query().All(ctx)
	return entSubscribesToModel(items), err
}

func (m *customSubscribeModel) FindByIds(ctx context.Context, ids []int64) ([]*Subscribe, error) {
	items, err := m.db.Subscribe.Query().Where(entsubscribe.IDIn(ids...)).All(ctx)
	return entSubscribesToModel(items), err
}

func (m *customSubscribeModel) CountByDefaultNodeGroup(ctx context.Context, nodeGroupId int64) (int64, error) {
	count, err := m.db.Subscribe.Query().Where(entsubscribe.NodeGroupID(nodeGroupId)).Count(ctx)
	return int64(count), err
}

func (m *customSubscribeModel) ClearAllNodeGroupIds(ctx context.Context) error {
	_, err := m.db.Subscribe.Update().SetNodeGroupIds([]int64{}).Save(ctx)
	return err
}

func (m *customSubscribeModel) QuerySubscribeMinSortByIds(ctx context.Context, ids []int64) (int64, error) {
	minSort, err := m.db.Subscribe.Query().Where(entsubscribe.IDIn(ids...)).Aggregate(ent.Min(entsubscribe.FieldSort)).Int(ctx)
	return int64(minSort), err
}

func (m *customSubscribeModel) QueryResetCycleSubscribeIds(ctx context.Context, resetCycle int) ([]int64, error) {
	return m.db.Subscribe.Query().Where(entsubscribe.ResetCycle(int64(resetCycle))).IDs(ctx)
}

func (m *customSubscribeModel) ClearCache(ctx context.Context, ids ...int64) error {
	if len(ids) == 0 {
		return nil
	}

	var cacheKeys []string
	for _, id := range ids {
		data, err := m.FindOne(ctx, id)
		if err != nil {
			return err
		}
		cacheKeys = append(cacheKeys, m.getCacheKeys(data)...)
	}
	return m.delCache(ctx, cacheKeys...)
}

func (m *customSubscribeModel) UpdateSort(ctx context.Context, data []*Subscribe) error {
	if len(data) == 0 {
		return nil
	}
	cacheKeys := m.batchGetCacheKeys(data...)
	for _, item := range data {
		if err := m.Update(ctx, item); err != nil {
			return err
		}
	}
	return m.delCache(ctx, cacheKeys...)
}

func (m *customSubscribeModel) QueryGroupList(ctx context.Context) (int64, []*Group, error) {
	total, err := m.db.SubscribeGroup.Query().Count(ctx)
	if err != nil {
		return 0, nil, err
	}
	list, err := m.db.SubscribeGroup.Query().All(ctx)
	return int64(total), entGroupsToModel(list), err
}

func (m *customSubscribeModel) CreateGroup(ctx context.Context, data *Group) error {
	created, err := m.db.SubscribeGroup.Create().SetName(data.Name).SetDescription(data.Description).Save(ctx)
	if err != nil {
		return err
	}
	data.Id = created.ID
	data.CreatedAt = created.CreatedAt
	data.UpdatedAt = created.UpdatedAt
	return nil
}

func (m *customSubscribeModel) UpdateGroup(ctx context.Context, data *Group) error {
	return m.db.SubscribeGroup.UpdateOneID(data.Id).SetName(data.Name).SetDescription(data.Description).Exec(ctx)
}

func (m *customSubscribeModel) DeleteGroup(ctx context.Context, id int64) error {
	err := m.db.SubscribeGroup.DeleteOneID(id).Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

func (m *customSubscribeModel) BatchDeleteGroup(ctx context.Context, ids []int64) error {
	_, err := m.db.SubscribeGroup.Delete().Where(entsubscribegroup.IDIn(ids...)).Exec(ctx)
	return err
}

// FilterList Filter Subscribe List
func (m *customSubscribeModel) FilterList(ctx context.Context, params *FilterParams) (int64, []*Subscribe, error) {
	if params == nil {
		params = &FilterParams{}
	}
	params.Normalize()

	buildQuery := func(lang string) *ent.SubscribeQuery {
		query := m.db.Subscribe.Query()

		if params.Search != "" {
			search := strings.TrimSpace(params.Search)
			query = query.Where(entsubscribe.Or(entsubscribe.NameContains(search), entsubscribe.DescriptionContains(search)))
		}
		if params.Show {
			query = query.Where(entsubscribe.Show(true))
		}
		if params.Sell {
			query = query.Where(entsubscribe.Sell(true))
		}

		if len(params.Ids) > 0 {
			query = query.Where(entsubscribe.IDIn(params.Ids...))
		}
		if len(params.Node) > 0 {
			query = query.Where(subscribeCommaSeparatedContainsAny(entsubscribe.FieldNodes, tool.Int64SliceToStringSlice(params.Node)))
		}

		if len(params.Tags) > 0 {
			query = query.Where(subscribeCommaSeparatedContainsAny(entsubscribe.FieldNodeTags, params.Tags))
		}
		if params.NodeGroupId != nil {
			query = query.Where(subscribeJSONContains(entsubscribe.FieldNodeGroupIds, *params.NodeGroupId))
		}
		if lang != "" {
			query = query.Where(entsubscribe.Language(lang))
		} else if params.DefaultLanguage {
			query = query.Where(entsubscribe.Language(""))
		}

		return query
	}

	queryFunc := func(lang string) (int64, []*Subscribe, error) {
		query := buildQuery(lang)
		total, err := query.Clone().Count(ctx)
		if err != nil {
			return 0, nil, err
		}
		list, err := query.Order(entsubscribe.BySort()).Limit(params.Size).Offset((params.Page - 1) * params.Size).All(ctx)
		return int64(total), entSubscribesToModel(list), err
	}

	total, list, err := queryFunc(params.Language)
	if err != nil {
		return 0, nil, err
	}

	if params.DefaultLanguage && total == 0 {
		total, list, err = queryFunc("")
		if err != nil {
			return 0, nil, err
		}
	}

	return total, list, nil
}

// FilterListByNodeGroups Filter subscribes by node groups
// Match if subscribe's node_group_id OR node_group_ids contains any of the provided node group IDs
func (m *customSubscribeModel) FilterListByNodeGroups(ctx context.Context, params *FilterByNodeGroupsParams) (int64, []*Subscribe, error) {
	if params == nil {
		params = &FilterByNodeGroupsParams{
			Page: 1,
			Size: 10,
		}
	}
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.Size <= 0 {
		params.Size = 10
	}

	query := m.db.Subscribe.Query()
	if len(params.NodeGroupIds) > 0 {
		predicates := []predicate.Subscribe{entsubscribe.NodeGroupIDIn(params.NodeGroupIds...)}
		for _, id := range params.NodeGroupIds {
			predicates = append(predicates, subscribeJSONContains(entsubscribe.FieldNodeGroupIds, id))
		}
		query = query.Where(entsubscribe.Or(predicates...))
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return 0, nil, err
	}
	list, err := query.Order(entsubscribe.BySort()).Limit(params.Size).Offset((params.Page - 1) * params.Size).All(ctx)
	return int64(total), entSubscribesToModel(list), err
}

func subscribeCommaSeparatedContainsAny(field string, values []string) predicate.Subscribe {
	predicates := make([]predicate.Subscribe, 0, len(values))
	for _, value := range values {
		v := value
		predicates = append(predicates, predicate.Subscribe(func(s *sql.Selector) {
			s.Where(sql.ExprP(fmt.Sprintf("FIND_IN_SET(?, %s)", field), v))
		}))
	}
	return entsubscribe.Or(predicates...)
}

func subscribeJSONContains(field string, id int64) predicate.Subscribe {
	return predicate.Subscribe(func(s *sql.Selector) {
		s.Where(sql.ExprP(fmt.Sprintf("JSON_CONTAINS(%s, ?)", field), fmt.Sprintf("%d", id)))
	})
}
