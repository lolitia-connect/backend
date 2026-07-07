package node

import (
	"context"

	"github.com/perfect-panel/server/ent"
	entnode "github.com/perfect-panel/server/ent/node"
	entserver "github.com/perfect-panel/server/ent/server"
	"github.com/redis/go-redis/v9"
)

var _ Model = (*customServerModel)(nil)

type (
	Model interface {
		serverModel
		NodeModel
		customCacheLogicModel
		customServerLogicModel
	}
	serverModel interface {
		InsertServer(ctx context.Context, data *Server) error
		FindOneServer(ctx context.Context, id int64) (*Server, error)
		UpdateServer(ctx context.Context, data *Server) error
		DeleteServer(ctx context.Context, id int64) error
		FindServerConfigOverride(ctx context.Context, serverId int64) (*ServerConfigOverride, error)
		SaveServerConfigOverride(ctx context.Context, data *ServerConfigOverride) error
		DeleteServerConfigOverride(ctx context.Context, serverId int64) error
		QueryServerList(ctx context.Context, ids []int64) (servers []*Server, err error)
	}

	NodeModel interface {
		InsertNode(ctx context.Context, data *Node) error
		FindOneNode(ctx context.Context, id int64) (*Node, error)
		UpdateNode(ctx context.Context, data *Node) error
		DeleteNode(ctx context.Context, id int64) error
	}

	customServerModel struct {
		*defaultServerModel
	}
	defaultServerModel struct {
		db    *ent.Client
		Cache *redis.Client
	}
)

func newServerModel(db *ent.Client, cache *redis.Client) *defaultServerModel {
	return &defaultServerModel{db: db, Cache: cache}
}

func NewModel(conn *ent.Client, cache *redis.Client) Model {
	return &customServerModel{defaultServerModel: newServerModel(conn, cache)}
}

func (m *defaultServerModel) InsertServer(ctx context.Context, data *Server) error {
	if data.Sort == 0 {
		maxSort, err := m.db.Server.Query().Aggregate(ent.Max(entserver.FieldSort)).Int(ctx)
		if err != nil {
			return err
		}
		data.Sort = maxSort + 1
	}
	created, err := m.db.Server.Create().
		SetName(data.Name).
		SetCountry(data.Country).
		SetCity(data.City).
		SetAddress(data.Address).
		SetSort(data.Sort).
		SetNillableProtocols(nilIfEmpty(data.Protocols)).
		SetNillableLastReportedAt(data.LastReportedAt).
		SetLongitude(data.Longitude).
		SetLatitude(data.Latitude).
		SetLongitudeCenter(data.LongitudeCenter).
		SetLatitudeCenter(data.LatitudeCenter).
		Save(ctx)
	if err != nil {
		return err
	}
	data.Id = created.ID
	data.CreatedAt = created.CreatedAt
	data.UpdatedAt = created.UpdatedAt
	return nil
}

func (m *defaultServerModel) FindOneServer(ctx context.Context, id int64) (*Server, error) {
	data, err := m.db.Server.Query().Where(entserver.ID(id)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return entToServer(data), nil
}

func (m *defaultServerModel) UpdateServer(ctx context.Context, data *Server) error {
	if err := m.ensureUniqueServerSort(ctx, data); err != nil {
		return err
	}
	return m.db.Server.UpdateOneID(data.Id).
		SetName(data.Name).
		SetCountry(data.Country).
		SetCity(data.City).
		SetAddress(data.Address).
		SetSort(data.Sort).
		SetNillableProtocols(nilIfEmpty(data.Protocols)).
		SetNillableLastReportedAt(data.LastReportedAt).
		SetLongitude(data.Longitude).
		SetLatitude(data.Latitude).
		SetLongitudeCenter(data.LongitudeCenter).
		SetLatitudeCenter(data.LatitudeCenter).
		Exec(ctx)
}

func (m *defaultServerModel) DeleteServer(ctx context.Context, id int64) error {
	old, err := m.FindOneServer(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err := m.db.Server.DeleteOneID(id).Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return err
	}
	_, err = m.db.Server.Update().Where(entserver.SortGT(old.Sort)).AddSort(-1).Save(ctx)
	return err
}

func (m *defaultServerModel) InsertNode(ctx context.Context, data *Node) error {
	if data.Sort == 0 {
		maxSort, err := m.db.Node.Query().Aggregate(ent.Max(entnode.FieldSort)).Int(ctx)
		if err != nil {
			return err
		}
		data.Sort = maxSort + 1
	}
	created, err := m.nodeCreate(data).Save(ctx)
	if err != nil {
		return err
	}
	data.Id = created.ID
	data.CreatedAt = created.CreatedAt
	data.UpdatedAt = created.UpdatedAt
	return nil
}

func (m *defaultServerModel) FindOneNode(ctx context.Context, id int64) (*Node, error) {
	data, err := m.db.Node.Query().Where(entnode.ID(id)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return entToNode(data), nil
}

func (m *defaultServerModel) UpdateNode(ctx context.Context, data *Node) error {
	if err := m.ensureUniqueNodeSort(ctx, data); err != nil {
		return err
	}
	return m.nodeUpdate(data).Exec(ctx)
}

func (m *defaultServerModel) DeleteNode(ctx context.Context, id int64) error {
	old, err := m.FindOneNode(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return err
	}
	if err := m.db.Node.DeleteOneID(id).Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return err
	}
	_, err = m.db.Node.Update().Where(entnode.SortGT(old.Sort)).AddSort(-1).Save(ctx)
	return err
}
