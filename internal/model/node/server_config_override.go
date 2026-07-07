package node

import (
	"context"
	"time"

	"github.com/perfect-panel/server/ent"
	entserverconfigoverride "github.com/perfect-panel/server/ent/serverconfigoverride"
)

type ServerConfigOverride struct {
	Id         int64
	ServerId   int64
	IPStrategy *string
	DNS        *string
	Block      *string
	Outbound   *string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (m *defaultServerModel) FindServerConfigOverride(ctx context.Context, serverId int64) (*ServerConfigOverride, error) {
	data, err := m.db.ServerConfigOverride.Query().Where(entserverconfigoverride.ServerID(serverId)).First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return entToServerConfigOverride(data), nil
}

func (m *defaultServerModel) SaveServerConfigOverride(ctx context.Context, data *ServerConfigOverride) error {
	old, err := m.FindServerConfigOverride(ctx, data.ServerId)
	if err != nil {
		return err
	}
	if old != nil {
		data.Id = old.Id
		data.CreatedAt = old.CreatedAt
		return m.db.ServerConfigOverride.UpdateOneID(old.Id).
			SetNillableIPStrategy(data.IPStrategy).
			SetNillableDNS(data.DNS).
			SetNillableBlock(data.Block).
			SetNillableOutbound(data.Outbound).
			Exec(ctx)
	}
	created, err := m.db.ServerConfigOverride.Create().
		SetServerID(data.ServerId).
		SetNillableIPStrategy(data.IPStrategy).
		SetNillableDNS(data.DNS).
		SetNillableBlock(data.Block).
		SetNillableOutbound(data.Outbound).
		Save(ctx)
	if err != nil {
		return err
	}
	data.Id = created.ID
	data.CreatedAt = created.CreatedAt
	data.UpdatedAt = created.UpdatedAt
	return nil
}

func (m *defaultServerModel) DeleteServerConfigOverride(ctx context.Context, serverId int64) error {
	_, err := m.db.ServerConfigOverride.Delete().Where(entserverconfigoverride.ServerID(serverId)).Exec(ctx)
	return err
}
