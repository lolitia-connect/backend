package node

import (
	"context"
	"fmt"

	"entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	entnode "github.com/perfect-panel/server/ent/node"
	"github.com/perfect-panel/server/ent/predicate"
	entserver "github.com/perfect-panel/server/ent/server"
)

func entToServer(data *ent.Server) *Server {
	if data == nil {
		return nil
	}
	protocols := ""
	if data.Protocols != nil {
		protocols = *data.Protocols
	}
	return &Server{
		Id:              data.ID,
		Name:            data.Name,
		Country:         data.Country,
		City:            data.City,
		Address:         data.Address,
		Sort:            data.Sort,
		Protocols:       protocols,
		LastReportedAt:  data.LastReportedAt,
		Longitude:       data.Longitude,
		Latitude:        data.Latitude,
		LongitudeCenter: data.LongitudeCenter,
		LatitudeCenter:  data.LatitudeCenter,
		CreatedAt:       data.CreatedAt,
		UpdatedAt:       data.UpdatedAt,
	}
}

func entServersToModel(list []*ent.Server) []*Server {
	items := make([]*Server, 0, len(list))
	for _, item := range list {
		items = append(items, entToServer(item))
	}
	return items
}

func entToNode(data *ent.Node) *Node {
	if data == nil {
		return nil
	}
	enabled := data.Enabled
	isHidden := data.IsHidden
	return &Node{
		Id:           data.ID,
		Name:         data.Name,
		Tags:         data.Tags,
		Port:         data.Port,
		Address:      data.Address,
		ServerId:     data.ServerID,
		Protocol:     data.Protocol,
		ProtocolId:   data.ProtocolID,
		Enabled:      &enabled,
		NodeType:     data.NodeType,
		IsHidden:     &isHidden,
		Sort:         data.Sort,
		NodeGroupIds: JSONInt64Slice(data.NodeGroupIds),
		CreatedAt:    data.CreatedAt,
		UpdatedAt:    data.UpdatedAt,
	}
}

func entNodesToModel(list []*ent.Node) []*Node {
	items := make([]*Node, 0, len(list))
	for _, item := range list {
		items = append(items, entToNode(item))
	}
	return items
}

func entToServerConfigOverride(data *ent.ServerConfigOverride) *ServerConfigOverride {
	if data == nil {
		return nil
	}
	return &ServerConfigOverride{
		Id:         data.ID,
		ServerId:   data.ServerID,
		IPStrategy: data.IPStrategy,
		DNS:        data.DNS,
		Block:      data.Block,
		Outbound:   data.Outbound,
		CreatedAt:  data.CreatedAt,
		UpdatedAt:  data.UpdatedAt,
	}
}

func (m *customServerModel) preloadNodeServers(ctx context.Context, nodes []*Node) error {
	serverIDs := make([]int64, 0, len(nodes))
	for _, item := range nodes {
		serverIDs = append(serverIDs, item.ServerId)
	}
	servers, err := m.QueryServerList(ctx, serverIDs)
	if err != nil {
		return err
	}
	serverMap := make(map[int64]*Server, len(servers))
	for _, item := range servers {
		serverMap[item.Id] = item
	}
	for _, item := range nodes {
		item.Server = serverMap[item.ServerId]
	}
	return nil
}

func (m *defaultServerModel) reorderServers(ctx context.Context) error {
	servers, err := m.db.Server.Query().Order(entserver.BySort(), entserver.ByID()).All(ctx)
	if err != nil {
		return err
	}
	for i, item := range servers {
		sort := i + 1
		if item.Sort != sort {
			if err := m.db.Server.UpdateOneID(item.ID).SetSort(sort).Exec(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *defaultServerModel) reorderNodes(ctx context.Context) error {
	nodes, err := m.db.Node.Query().Order(entnode.BySort(), entnode.ByID()).All(ctx)
	if err != nil {
		return err
	}
	for i, item := range nodes {
		sort := i + 1
		if item.Sort != sort {
			if err := m.db.Node.UpdateOneID(item.ID).SetSort(sort).Exec(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func commaSeparatedContains(field string, value string) predicate.Node {
	return predicate.Node(func(s *sql.Selector) {
		s.Where(sql.ExprP(fmt.Sprintf("FIND_IN_SET(?, %s)", field), value))
	})
}

func commaSeparatedContainsAny(field string, values []string) predicate.Node {
	predicates := make([]predicate.Node, 0, len(values))
	for _, value := range values {
		predicates = append(predicates, commaSeparatedContains(field, value))
	}
	return entnode.Or(predicates...)
}

func nodeNameGBKOrder() entnode.OrderOption {
	return func(s *sql.Selector) {
		s.OrderExpr(sql.Expr("CONVERT(name USING gbk) ASC"))
	}
}

func serverNameGBKOrder() entserver.OrderOption {
	return func(s *sql.Selector) {
		s.OrderExpr(sql.Expr("CONVERT(name USING gbk) ASC"))
	}
}
