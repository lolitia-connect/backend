package node

import (
	"context"

	"entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	entnode "github.com/perfect-panel/server/ent/node"
	entserver "github.com/perfect-panel/server/ent/server"
)

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

func (m *defaultServerModel) ensureUniqueServerSort(ctx context.Context, data *Server) error {
	count, err := m.db.Server.Query().Where(entserver.Sort(data.Sort), entserver.IDNEQ(data.Id)).Count(ctx)
	if err != nil || count <= 1 {
		return err
	}
	if err := m.reorderServers(ctx); err != nil {
		return err
	}
	maxSort, err := m.db.Server.Query().Aggregate(ent.Max(entserver.FieldSort)).Int(ctx)
	if err != nil {
		return err
	}
	data.Sort = maxSort + 1
	return nil
}

func (m *defaultServerModel) ensureUniqueNodeSort(ctx context.Context, data *Node) error {
	count, err := m.db.Node.Query().Where(entnode.Sort(data.Sort), entnode.IDNEQ(data.Id)).Count(ctx)
	if err != nil || count <= 1 {
		return err
	}
	if err := m.reorderNodes(ctx); err != nil {
		return err
	}
	maxSort, err := m.db.Node.Query().Aggregate(ent.Max(entnode.FieldSort)).Int(ctx)
	if err != nil {
		return err
	}
	data.Sort = maxSort + 1
	return nil
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
