package node

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	entnode "github.com/perfect-panel/server/ent/node"
	"github.com/perfect-panel/server/ent/predicate"
	entserver "github.com/perfect-panel/server/ent/server"
)

type customServerLogicModel interface {
	FilterServerList(ctx context.Context, params *FilterParams) (int64, []*Server, error)
	FilterNodeList(ctx context.Context, params *FilterNodeParams) (int64, []*Node, error)
	QueryNodeSorts(ctx context.Context) ([]SortItem, error)
	QueryServerSorts(ctx context.Context) ([]SortItem, error)
	UpdateNodeSort(ctx context.Context, id int64, sort int64) error
	UpdateServerSort(ctx context.Context, id int64, sort int64) error
	UpdateServerLastReportedAt(ctx context.Context, id int64, t time.Time) error
	DeleteServerWithOverrides(ctx context.Context, serverId int64) error
	QueryNodeTags(ctx context.Context) ([]string, error)
	CountEnabledNodes(ctx context.Context) (int64, error)
	CountServersByReportStatus(ctx context.Context, cutoff time.Time) (int64, int64, error)
	QueryServerAddresses(ctx context.Context) ([]string, error)
	QueryEnabledNodeProtocols(ctx context.Context) ([]string, error)
	ClearNodeCache(ctx context.Context, params *FilterNodeParams) error
	ClearServerCache(ctx context.Context, serverId int64) error
	ClearServerAllCache(ctx context.Context) error
	CountByNodeGroupId(ctx context.Context, nodeGroupId int64) (int64, error)
	QueryEnabledVisibleNodes(ctx context.Context) ([]*Node, error)
	QueryEnabledVisibleNodesByIds(ctx context.Context, ids []int64) ([]*Node, error)
	ClearAllNodeGroupIds(ctx context.Context) error
	SortNodesByName(ctx context.Context) error
	SortServersByName(ctx context.Context) error
}

const (
	// ServerCacheTTL TTL for node hot-path server caches (server config and user list)
	ServerCacheTTL = 5 * time.Minute

	// ServerUserListCacheKey Server User List Cache Key
	ServerUserListCacheKey = "server:user:"

	// ServerConfigCacheKey Server Config Cache Key
	ServerConfigCacheKey = "server:config:"
)

// FilterParams Filter Server Params
type FilterParams struct {
	Page   int
	Size   int
	Ids    []int64 // Server IDs
	Search string
}

type FilterNodeParams struct {
	Page         int      // Page Number
	Size         int      // Page Size
	NodeId       []int64  // Node IDs
	ServerId     []int64  // Server IDs
	Tag          []string // Tags
	NodeGroupIds []int64  // Node Group IDs
	Search       string   // Search Address or Name
	Protocol     string   // Protocol
	ProtocolId   string   // Protocol Instance ID
	Preload      bool     // Preload Server
	Enabled      *bool    // Enabled
	IsHidden     *bool    // IsHidden - when not nil, filter by hidden status
}

type SortItem struct {
	Id   int64
	Sort int64
}

// FilterServerList Filter Server List
func (m *customServerModel) FilterServerList(ctx context.Context, params *FilterParams) (int64, []*Server, error) {
	if params == nil {
		params = &FilterParams{
			Page: 1,
			Size: 10,
		}
	}
	query := m.db.Server.Query()
	if params.Search != "" {
		search := strings.TrimSpace(params.Search)
		query = query.Where(entserver.Or(entserver.NameContains(search), entserver.AddressContains(search)))
	}
	if len(params.Ids) > 0 {
		query = query.Where(entserver.IDIn(params.Ids...))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return 0, nil, err
	}
	list, err := query.Order(entserver.BySort(), entserver.ByID()).Limit(params.Size).Offset((params.Page - 1) * params.Size).All(ctx)
	return int64(total), entServersToModel(list), err
}

func (m *customServerModel) QueryServerList(ctx context.Context, ids []int64) (servers []*Server, err error) {
	list, err := m.db.Server.Query().Where(entserver.IDIn(ids...)).All(ctx)
	return entServersToModel(list), err
}

func (m *customServerModel) QueryServerSorts(ctx context.Context) ([]SortItem, error) {
	list, err := m.db.Server.Query().Order(entserver.BySort(), entserver.ByID()).All(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]SortItem, 0, len(list))
	for _, item := range list {
		items = append(items, SortItem{Id: item.ID, Sort: int64(item.Sort)})
	}
	return items, nil
}

func (m *customServerModel) UpdateServerSort(ctx context.Context, id int64, sort int64) error {
	server, err := m.FindOneServer(ctx, id)
	if err != nil {
		return err
	}
	server.Sort = int(sort)
	return m.UpdateServer(ctx, server)
}

// UpdateServerLastReportedAt updates only the last_reported_at field,
// avoiding full-row updates that cause optimistic locking conflicts
// when multiple nodes report status simultaneously.
func (m *customServerModel) UpdateServerLastReportedAt(ctx context.Context, id int64, t time.Time) error {
	return m.db.Server.UpdateOneID(id).SetLastReportedAt(t).Exec(ctx)
}

func (m *customServerModel) DeleteServerWithOverrides(ctx context.Context, serverId int64) error {
	if err := m.DeleteServerConfigOverride(ctx, serverId); err != nil {
		return err
	}
	return m.DeleteServer(ctx, serverId)
}

// FilterNodeList Filter Node List
func (m *customServerModel) FilterNodeList(ctx context.Context, params *FilterNodeParams) (int64, []*Node, error) {
	if params == nil {
		params = &FilterNodeParams{
			Page: 1,
			Size: 10,
		}
	}
	query := m.db.Node.Query()
	if params.Search != "" {
		search := strings.TrimSpace(params.Search)
		predicates := []predicate.Node{entnode.NameContains(search), entnode.AddressContains(search), commaSeparatedContains(entnode.FieldTags, search)}
		if port, err := strconv.ParseUint(search, 10, 16); err == nil {
			predicates = append(predicates, entnode.Port(uint16(port)))
		}
		query = query.Where(entnode.Or(predicates...))
	}
	if len(params.NodeId) > 0 {
		query = query.Where(entnode.IDIn(params.NodeId...))
	}
	if len(params.ServerId) > 0 {
		query = query.Where(entnode.ServerIDIn(params.ServerId...))
	}
	if len(params.Tag) > 0 {
		query = query.Where(commaSeparatedContainsAny(entnode.FieldTags, params.Tag))
	}
	if len(params.NodeGroupIds) > 0 {
		predicates := make([]predicate.Node, 0, len(params.NodeGroupIds))
		for _, gid := range params.NodeGroupIds {
			id := gid
			predicates = append(predicates, predicate.Node(func(s *sql.Selector) {
				s.Where(sql.ExprP("JSON_CONTAINS(node_group_ids, ?)", fmt.Sprintf("[%d]", id)))
			}))
		}
		query = query.Where(entnode.Or(predicates...))
	}
	if params.ProtocolId != "" {
		query = query.Where(entnode.ProtocolID(params.ProtocolId))
	}
	if params.Protocol != "" {
		query = query.Where(entnode.Protocol(params.Protocol))
	}

	if params.Enabled != nil {
		query = query.Where(entnode.Enabled(*params.Enabled))
	}

	if params.IsHidden != nil {
		query = query.Where(entnode.IsHidden(*params.IsHidden))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return 0, nil, err
	}
	list, err := query.Order(entnode.BySort(), entnode.ByID()).Limit(params.Size).Offset((params.Page - 1) * params.Size).All(ctx)
	if err != nil {
		return 0, nil, err
	}
	nodes := entNodesToModel(list)
	if params.Preload {
		if err := m.preloadNodeServers(ctx, nodes); err != nil {
			return 0, nil, err
		}
	}
	return int64(total), nodes, nil
}

func (m *customServerModel) QueryNodeSorts(ctx context.Context) ([]SortItem, error) {
	list, err := m.db.Node.Query().Order(entnode.BySort(), entnode.ByID()).All(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]SortItem, 0, len(list))
	for _, item := range list {
		items = append(items, SortItem{Id: item.ID, Sort: int64(item.Sort)})
	}
	return items, nil
}

func (m *customServerModel) CountByNodeGroupId(ctx context.Context, nodeGroupId int64) (int64, error) {
	count, err := m.db.Node.Query().Where(predicate.Node(func(s *sql.Selector) {
		s.Where(sql.ExprP("JSON_CONTAINS(node_group_ids, ?)", fmt.Sprintf("[%d]", nodeGroupId)))
	})).Count(ctx)
	return int64(count), err
}

func (m *customServerModel) QueryEnabledVisibleNodes(ctx context.Context) ([]*Node, error) {
	items, err := m.db.Node.Query().Where(entnode.Enabled(true), entnode.IsHidden(false)).All(ctx)
	return entNodesToModel(items), err
}

func (m *customServerModel) QueryEnabledVisibleNodesByIds(ctx context.Context, ids []int64) ([]*Node, error) {
	items, err := m.db.Node.Query().Where(entnode.IDIn(ids...), entnode.Enabled(true), entnode.IsHidden(false)).All(ctx)
	return entNodesToModel(items), err
}

func (m *customServerModel) ClearAllNodeGroupIds(ctx context.Context) error {
	_, err := m.db.Node.Update().SetNodeGroupIds([]int64{}).Save(ctx)
	return err
}

func (m *customServerModel) UpdateNodeSort(ctx context.Context, id int64, sort int64) error {
	node, err := m.FindOneNode(ctx, id)
	if err != nil {
		return err
	}
	node.Sort = int(sort)
	return m.UpdateNode(ctx, node)
}

func (m *customServerModel) QueryNodeTags(ctx context.Context) ([]string, error) {
	return m.db.Node.Query().Select(entnode.FieldTags).Strings(ctx)
}

func (m *customServerModel) SortNodesByName(ctx context.Context) error {
	nodes, err := m.db.Node.Query().Order(nodeNameGBKOrder()).All(ctx)
	if err != nil {
		return err
	}
	for i, n := range nodes {
		if n.Sort != i {
			if err := m.db.Node.UpdateOneID(n.ID).SetSort(i).Exec(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *customServerModel) SortServersByName(ctx context.Context) error {
	servers, err := m.db.Server.Query().Order(serverNameGBKOrder()).All(ctx)
	if err != nil {
		return err
	}
	for i, s := range servers {
		if s.Sort != i {
			if err := m.db.Server.UpdateOneID(s.ID).SetSort(i).Exec(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *customServerModel) CountEnabledNodes(ctx context.Context) (int64, error) {
	total, err := m.db.Node.Query().Where(entnode.Enabled(true)).Count(ctx)
	return int64(total), err
}

func (m *customServerModel) CountServersByReportStatus(ctx context.Context, cutoff time.Time) (int64, int64, error) {
	online, err := m.db.Server.Query().Where(entserver.LastReportedAtGT(cutoff)).Count(ctx)
	if err != nil {
		return 0, 0, err
	}

	offline, err := m.db.Server.Query().Where(entserver.Or(entserver.LastReportedAtLTE(cutoff), entserver.LastReportedAtIsNil())).Count(ctx)
	if err != nil {
		return 0, 0, err
	}

	return int64(online), int64(offline), nil
}

func (m *customServerModel) QueryServerAddresses(ctx context.Context) ([]string, error) {
	return m.db.Server.Query().Select(entserver.FieldAddress).Strings(ctx)
}

func (m *customServerModel) QueryEnabledNodeProtocols(ctx context.Context) ([]string, error) {
	return m.db.Node.Query().Where(entnode.Enabled(true)).Select(entnode.FieldProtocol).Strings(ctx)
}
