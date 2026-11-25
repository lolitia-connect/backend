package server

import (
	"database/sql"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func TestServerCatalogCombinesHealthAndDistinctSubscriptions(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	cache := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, redisClient)}

	now := time.Now()
	protocols := `[{"id":"primary","name":"Primary","type":"vless","enable":true},` +
		`{"id":"secondary","name":"Secondary","type":"vless","enable":true,"ratio":2},` +
		`{"id":"retired","type":"trojan","enable":false}]`
	active := client.Server.Create().SetName("Active").SetSort(1).SetProtocols(protocols).SetLastReportedAt(now).SaveX(ctx)
	warning := client.Server.Create().SetName("Delayed").SetSort(2).SetLastReportedAt(now.Add(-4 * time.Minute)).SaveX(ctx)
	offline := client.Server.Create().SetName("Offline").SetSort(3).SetLastReportedAt(now.Add(-10 * time.Minute)).SaveX(ctx)
	client.Server.Create().SetName("Broken configuration").SetSort(4).SetProtocols("invalid JSON").SaveX(ctx)
	store := service.Store.Node()
	if err := store.UpdateStatusCache(ctx, active.ID, &node.Status{Cpu: 0.25, Mem: 0.5}); err != nil {
		t.Fatal(err)
	}
	for key, online := range map[string]node.OnlineUserSubscribe{
		"vless:primary":   {1: {"192.0.2.1"}, 2: {"192.0.2.2"}},
		"vless:secondary": {1: {"192.0.2.3"}, 3: {"192.0.2.4"}},
		"trojan:retired":  {4: {"192.0.2.5"}},
	} {
		if err := store.UpdateOnlineUserSubscribe(ctx, active.ID, key, online); err != nil {
			t.Fatal(err)
		}
	}

	logic := NewGetServerNodeListLogic(ctx, service)
	result, err := logic.GetServerNodeList()
	if err != nil || len(result.List) != 3 {
		t.Fatalf("server catalog = %+v, %v", result, err)
	}
	first := result.List[0]
	if first.Id != active.ID || first.Status != "online" || first.Cpu != 0.25 || first.Mem != 0.5 || first.OnlineUsers != 3 {
		t.Fatalf("active server summary = %+v", first)
	}
	if p := first.Protocols; len(p) != 2 || p[0].Id != "primary" || p[0].Ratio != 1 || p[1].Id != "secondary" || p[1].Ratio != 2 {
		t.Fatalf("protocol metadata = %+v", p)
	}
	if second, third := result.List[1], result.List[2]; second.Id != warning.ID || second.Status != "warning" || third.Id != offline.ID || third.Status != "offline" {
		t.Fatalf("delayed and offline servers = %+v, %+v", second, third)
	}

	cache.FastForward(node.Expiry + time.Second)
	result, err = logic.GetServerNodeList()
	if err != nil || len(result.List) != 3 || result.List[0].OnlineUsers != 0 || result.List[0].Cpu != 0 || result.List[0].Mem != 0 {
		t.Fatalf("expired telemetry is still in the server catalog: %+v, %v", result, err)
	}
}
