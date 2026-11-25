package server

import (
	"database/sql"
	"fmt"
	"slices"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	entserver "github.com/perfect-panel/server/ent/server"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func TestAdminServerAndNodeLifecycleInvalidatesCachedConfiguration(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	cache := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { _ = rds.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, rds), Redis: rds}

	create := NewCreateServerLogic(ctx, service)
	if err := create.CreateServer(&types.CreateServerRequest{Name: "Invalid protocol", Protocols: []types.Protocol{{Type: "unknown"}}}); err == nil {
		t.Fatal("unsupported protocol was persisted")
	}
	definition := types.Protocol{Id: "web", Type: "VLESS", Port: 443, Enable: true, Transport: "tcp"}
	for _, name := range []string{"Primary", "Backup"} {
		if err := create.CreateServer(&types.CreateServerRequest{Name: name, Country: "JP", City: "Tokyo", Address: "192.0.2.10", Protocols: []types.Protocol{definition}}); err != nil {
			t.Fatal(err)
		}
	}
	primary := client.Server.Query().Where(entserver.Name("Primary")).OnlyX(ctx)
	backup := client.Server.Query().Where(entserver.Name("Backup")).OnlyX(ctx)
	if primary.Sort >= backup.Sort || client.Server.Query().CountX(ctx) != 2 {
		t.Fatal("server creation assigned the wrong order or persisted the invalid entry")
	}
	getProtocols := NewGetServerProtocolsLogic(ctx, service)
	got, err := getProtocols.GetServerProtocols(&types.GetServerProtocolsRequest{Id: primary.ID})
	if err != nil || len(got.Protocols) != 1 || got.Protocols[0].Type != "vless" || got.Protocols[0].Id != "web" {
		t.Fatalf("stored server protocol = %+v, %v", got, err)
	}

	cacheKey := func(id int64) string { return fmt.Sprintf("%s%d:vless:web", node.ServerConfigCacheKey, id) }
	seed := func(ids ...int64) {
		t.Helper()
		for _, id := range ids {
			if err := cache.Set(cacheKey(id), "cached configuration"); err != nil {
				t.Fatal(err)
			}
		}
	}
	seed(primary.ID)
	definition.Port = 8443
	if err := NewUpdateServerLogic(ctx, service).UpdateServer(&types.UpdateServerRequest{
		Id: primary.ID, Name: "Primary updated", Country: "JP", City: "Tokyo", Address: primary.Address, Protocols: []types.Protocol{definition},
	}); err != nil {
		t.Fatal(err)
	}
	if cache.Exists(cacheKey(primary.ID)) {
		t.Fatal("server edit kept stale configuration in Redis")
	}
	if err := service.Store.Node().UpdateServerLastReportedAt(ctx, primary.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := service.Store.Node().UpdateStatusCache(ctx, primary.ID, &node.Status{Cpu: 0.25}); err != nil {
		t.Fatal(err)
	}
	listed, err := NewFilterServerListLogic(ctx, service).FilterServerList(&types.FilterServerListRequest{Page: 1, Size: 10, Search: "updated"})
	if err != nil || listed.Total != 1 || len(listed.List) != 1 {
		t.Fatalf("filtered servers = %+v, %v", listed, err)
	}
	if entry := listed.List[0]; entry.Id != primary.ID || entry.Status.Status != "online" || entry.Status.Cpu != 0.25 || len(entry.Protocols) != 1 || entry.Protocols[0].Port != 8443 {
		t.Fatalf("updated server = %+v", entry)
	}

	enabled, disabled := true, false
	seed(primary.ID)
	if err := NewCreateNodeLogic(ctx, service).CreateNode(&types.CreateNodeRequest{
		Name: "Tokyo edge", Address: "tokyo.example.test", Port: 443, ServerId: primary.ID, Protocol: "vless", ProtocolId: "web",
		Enabled: &enabled, NodeType: "landing", Tags: []string{"asia", "premium", "asia"}, NodeGroupIds: []string{"12"},
	}); err != nil {
		t.Fatal(err)
	}
	if cache.Exists(cacheKey(primary.ID)) {
		t.Fatal("creating a node kept stale configuration")
	}
	nodes := NewFilterNodeListLogic(ctx, service)
	result, err := nodes.FilterNodeList(&types.FilterNodeListRequest{Page: 1, Size: 10})
	if err != nil || result.Total != 1 || len(result.List) != 1 {
		t.Fatalf("node catalog = %+v, %v", result, err)
	}
	entry := result.List[0]
	if entry.ServerId != primary.ID || !slices.Equal(entry.Tags, []string{"asia", "premium"}) || !slices.Equal(entry.NodeGroupIds, []string{"12"}) {
		t.Fatalf("new node = %+v", entry)
	}
	if err := NewToggleNodeStatusLogic(ctx, service).ToggleNodeStatus(&types.ToggleNodeStatusRequest{Id: entry.Id, Enable: &disabled}); err != nil {
		t.Fatal(err)
	}
	if client.Node.GetX(ctx, entry.Id).Enabled {
		t.Fatal("node still enabled after it was disabled")
	}
	seed(primary.ID, backup.ID)
	if err := NewUpdateNodeLogic(ctx, service).UpdateNode(&types.UpdateNodeRequest{
		Id: entry.Id, Name: entry.Name, ServerId: backup.ID, Protocol: "vless", ProtocolId: "web", Address: entry.Address, Port: entry.Port, Enabled: &enabled,
	}); err != nil {
		t.Fatal(err)
	}
	if current := client.Node.GetX(ctx, entry.Id); current.ServerID != backup.ID || !current.Enabled || cache.Exists(cacheKey(primary.ID)) || cache.Exists(cacheKey(backup.ID)) {
		t.Fatalf("node move did not persist and invalidate both servers: %+v", current)
	}
	if err := NewDeleteServerLogic(ctx, service).DeleteServer(&types.DeleteServerRequest{Id: backup.ID}); err != nil {
		t.Fatal(err)
	}
	if client.Node.Query().CountX(ctx) != 0 || client.Server.Query().OnlyX(ctx).ID != primary.ID {
		t.Fatal("server deletion did not just remove that server and its node")
	}
}
