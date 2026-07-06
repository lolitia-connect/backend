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
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
)

type customServerLogicModel interface {
	FilterServerList(ctx context.Context, params *FilterParams) (int64, []*Server, error)
	FilterNodeList(ctx context.Context, params *FilterNodeParams) (int64, []*Node, error)
	QueryNodeSorts(ctx context.Context) ([]SortItem, error)
	QueryServerSorts(ctx context.Context) ([]SortItem, error)
	UpdateNodeSort(ctx context.Context, id int64, sort int64) error
	UpdateServerSort(ctx context.Context, id int64, sort int64) error
	UpdateServerLastReportedAt(ctx context.Context, id int64, t time.Time) error
	QueryNodeTags(ctx context.Context) ([]string, error)
	CountEnabledNodes(ctx context.Context) (int64, error)
	CountServersByReportStatus(ctx context.Context, cutoff time.Time) (int64, int64, error)
	QueryServerAddresses(ctx context.Context) ([]string, error)
	QueryEnabledNodeProtocols(ctx context.Context) ([]string, error)
	ClearNodeCache(ctx context.Context, params *FilterNodeParams) error
	ClearServerCache(ctx context.Context, serverId int64) error
	ClearServerAllCache(ctx context.Context) error
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
		query = query.Where(entserver.Or(entserver.NameHasPrefix(search), entserver.AddressHasPrefix(search)))
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

// ClearNodeCache Clear Node Cache
func (m *customServerModel) ClearNodeCache(ctx context.Context, params *FilterNodeParams) error {
	_, nodes, err := m.FilterNodeList(ctx, params)
	if err != nil {
		return err
	}
	var cacheKeys []string
	for _, node := range nodes {
		// Scan all protocol variants of user list and config cache
		patterns := []string{
			fmt.Sprintf("%s%d:*", ServerUserListCacheKey, node.ServerId),
			fmt.Sprintf("%s%d:*", ServerConfigCacheKey, node.ServerId),
		}
		// Also delete legacy user-list key written before protocol was added to the key.
		cacheKeys = append(cacheKeys, fmt.Sprintf("%s%d", ServerUserListCacheKey, node.ServerId))
		for _, pattern := range patterns {
			var cursor uint64
			for {
				keys, newCursor, err := m.Cache.Scan(ctx, cursor, pattern, 100).Result()
				if err != nil {
					return err
				}
				if len(keys) > 0 {
					cacheKeys = append(cacheKeys, keys...)
				}
				cursor = newCursor
				if cursor == 0 {
					break
				}
			}
		}
	}

	if len(cacheKeys) > 0 {
		cacheKeys = tool.RemoveDuplicateElements(cacheKeys...)
		return m.Cache.Del(ctx, cacheKeys...).Err()
	}
	return nil
}

// ClearServerCache Clear Server Cache
func (m *customServerModel) ClearServerCache(ctx context.Context, serverId int64) error {
	var cacheKeys []string
	// Scan all protocol variants of both user list and config cache
	patterns := []string{
		fmt.Sprintf("%s%d:*", ServerUserListCacheKey, serverId),
		fmt.Sprintf("%s%d:*", ServerConfigCacheKey, serverId),
	}
	// Also delete legacy user-list key written before protocol was added to the key.
	cacheKeys = append(cacheKeys, fmt.Sprintf("%s%d", ServerUserListCacheKey, serverId))
	for _, pattern := range patterns {
		var cursor uint64
		for {
			keys, newCursor, err := m.Cache.Scan(ctx, cursor, pattern, 100).Result()
			if err != nil {
				return err
			}
			if len(keys) > 0 {
				cacheKeys = append(cacheKeys, keys...)
			}
			cursor = newCursor
			if cursor == 0 {
				break
			}
		}
	}

	if len(cacheKeys) > 0 {
		cacheKeys = tool.RemoveDuplicateElements(cacheKeys...)
		return m.Cache.Del(ctx, cacheKeys...).Err()
	}
	return nil
}

func (m *customServerModel) ClearServerAllCache(ctx context.Context) error {
	var cursor uint64
	var keys []string
	prefix := ServerUserListCacheKey + "*"
	for {
		scanKeys, newCursor, err := m.Cache.Scan(ctx, cursor, prefix, 999).Result()
		if err != nil {
			logger.Error(ctx, fmt.Sprintf("ClearServerAllCache err:%v", err))
			break
		}
		logger.Info(ctx, fmt.Sprintf("ClearServerAllCache query keys:%v", scanKeys))
		keys = append(keys, scanKeys...)
		cursor = newCursor
		if cursor == 0 {
			break
		}
	}
	if len(keys) > 0 {
		logger.Info(ctx, fmt.Sprintf("ClearServerAllCache keys:%v", keys))
		return m.Cache.Del(ctx, keys...).Err()
	}
	return nil
}
