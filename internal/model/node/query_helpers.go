package node

import (
	"context"
	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	entnode "github.com/perfect-panel/server/ent/node"
	"github.com/perfect-panel/server/ent/predicate"
	"github.com/perfect-panel/server/pkg/orm"
)

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

func nodeGroupContains(id int64) predicate.Node {
	return func(s *sql.Selector) {
		s.Where(sqljson.ValueContains(entnode.FieldNodeGroupIds, id))
	}
}

func commaSeparatedContainsAny(field string, values []string) predicate.Node {
	predicates := make([]predicate.Node, 0, len(values))
	for _, value := range values {
		predicates = append(predicates, orm.CSVMember(field, value))
	}
	return entnode.Or(predicates...)
}
