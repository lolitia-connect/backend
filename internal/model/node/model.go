package node

import (
	"context"
	"database/sql/driver"
	"fmt"
	"sort"
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
	UpdateNodeSort(ctx context.Context, id int64, sort int64) error
	UpdateServerSort(ctx context.Context, id int64, sort int64) error
	UpdateServerLastReportedAt(ctx context.Context, id int64, t time.Time) error
	UpdateServerLastReportedAtBatch(ctx context.Context, reports map[int64]time.Time) error
	ApplyReportedCertPin(ctx context.Context, serverId int64, protocolType, fingerprint string) (bool, error)
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

func (m *customServerModel) UpdateServerSort(ctx context.Context, id int64, sort int64) error {
	server, err := m.db.Server.Query().Where(entserver.ID(id)).Only(ctx)
	if err != nil {
		return err
	}
	if err := m.db.Server.UpdateOneID(id).SetSort(int(sort)).Exec(ctx); err != nil {
		return err
	}
	if m.Cache != nil {
		return m.ClearServerCache(ctx, server.ID)
	}
	return nil
}

// UpdateServerLastReportedAt updates only the last_reported_at field,
// avoiding full-row updates that cause optimistic locking conflicts
// when multiple nodes report status simultaneously.
func (m *customServerModel) UpdateServerLastReportedAt(ctx context.Context, id int64, t time.Time) error {
	return m.db.Server.UpdateOneID(id).SetLastReportedAt(t).Exec(ctx)
}

func (m *customServerModel) UpdateServerLastReportedAtBatch(ctx context.Context, reports map[int64]time.Time) error {
	ids := make([]int64, 0, len(reports))
	for id := range reports {
		if id > 0 {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(first, second int) bool { return ids[first] < ids[second] })
	for offset := 0; offset < len(ids); offset += 200 {
		batch := ids[offset:min(offset+200, len(ids))]
		arguments := make([]driver.Value, len(batch))
		for index, id := range batch {
			arguments[index] = id
		}
		statement := sql.Dialect(m.db.Driver().Dialect()).Update(entserver.Table).
			Set(entserver.FieldLastReportedAt, sql.ExprFunc(func(builder *sql.Builder) {
				builder.WriteString("CASE")
				for _, id := range batch {
					builder.WriteString(" WHEN ").Ident(entserver.FieldID).WriteString(" = ").Arg(id).
						WriteString(" AND (").Ident(entserver.FieldLastReportedAt).WriteString(" IS NULL OR ").
						Ident(entserver.FieldLastReportedAt).WriteString(" < ").Arg(reports[id]).WriteString(") THEN ").Arg(reports[id])
				}
				builder.WriteString(" ELSE ").Ident(entserver.FieldLastReportedAt).WriteString(" END")
			})).Where(sql.InValues(entserver.FieldID, arguments...))
		query, parameters := statement.Query()
		if err := m.db.Driver().Exec(ctx, query, parameters, nil); err != nil {
			return err
		}
	}
	return nil
}

// ApplyReportedCertPin folds a node-reported certificate fingerprint into the
// server's protocols JSON. Like UpdateServerLastReportedAt it touches a single
// column, so a heartbeat cannot collide with a concurrent admin edit, and it
// reports whether anything changed so the caller can skip cache invalidation on
// the steady-state heartbeats that make up almost all traffic.
func (m *customServerModel) ApplyReportedCertPin(ctx context.Context, serverId int64, protocolType, fingerprint string) (bool, error) {
	server, err := m.FindOneServer(ctx, serverId)
	if err != nil {
		return false, err
	}
	changed, err := server.ApplyReportedCertPin(protocolType, fingerprint)
	if err != nil || !changed {
		return false, err
	}
	return true, m.db.Server.UpdateOneID(serverId).SetNillableProtocols(nilIfEmpty(server.Protocols)).Exec(ctx)
}

func (m *customServerModel) DeleteServerWithOverrides(ctx context.Context, serverId int64) error {
	transaction, err := m.db.Tx(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	model := NewModel(transaction.Client(), m.Cache)
	nodes, err := transaction.Client().Node.Query().Where(entnode.ServerID(serverId)).Order(entnode.BySort(), entnode.ByID()).All(ctx)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if err := model.DeleteNode(ctx, node.ID); err != nil {
			return err
		}
	}
	if err := model.DeleteServerConfigOverride(ctx, serverId); err != nil {
		return err
	}
	if err := model.DeleteServer(ctx, serverId); err != nil {
		return err
	}
	return transaction.Commit()
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
	node, err := m.db.Node.Query().Where(entnode.ID(id)).Only(ctx)
	if err != nil {
		return err
	}
	if err := m.db.Node.UpdateOneID(id).SetSort(int(sort)).Exec(ctx); err != nil {
		return err
	}
	if m.Cache != nil {
		return m.ClearServerCache(ctx, node.ServerID)
	}
	return nil
}

func (m *customServerModel) QueryNodeTags(ctx context.Context) ([]string, error) {
	return m.db.Node.Query().Select(entnode.FieldTags).Strings(ctx)
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
