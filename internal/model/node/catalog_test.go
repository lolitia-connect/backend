package node

import (
	"fmt"
	"slices"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestNodeCatalogFiltersAndPreloadsPersistedServers(t *testing.T) {
	ctx := t.Context()
	client := deletionClient(t)
	model := NewModel(client, nil)
	primary := client.Server.Create().SetName("edge primary").SetAddress("primary.example.test").SetSort(2).SaveX(ctx)
	fallback := client.Server.Create().SetName("fallback").SetAddress("edge.example.test").SetSort(1).SaveX(ctx)
	other := client.Server.Create().SetName("internal").SetSort(3).SaveX(ctx)

	total, servers, err := model.FilterServerList(ctx, &FilterParams{Page: 2, Size: 1, Search: " edge ", Ids: []int64{primary.ID, fallback.ID, other.ID}})
	if err != nil || total != 2 || len(servers) != 1 || servers[0].Id != primary.ID {
		t.Fatalf("filtered server page = %v (total %d), %v; want server %d out of 2", servers, total, err, primary.ID)
	}

	for _, item := range []struct {
		id, server int64
		sort       int
		instance   string
		enabled    bool
		hidden     bool
	}{
		{id: 10, server: primary.ID, sort: 4, instance: "2", enabled: true},
		{id: 20, server: primary.ID, sort: 4, instance: "2", enabled: true},
		{id: 30, server: primary.ID, sort: 1, instance: "2", enabled: true, hidden: true},
		{id: 40, server: primary.ID, sort: 2, instance: "2"},
		{id: 50, server: primary.ID, sort: 3, instance: "1", enabled: true},
		{id: 60, server: fallback.ID, sort: 5, instance: "2", enabled: true},
	} {
		client.Node.Create().SetID(item.id).SetServerID(item.server).SetSort(item.sort).
			SetName(fmt.Sprintf("node %d", item.id)).SetProtocol("vless").SetProtocolID(item.instance).
			SetEnabled(item.enabled).SetIsHidden(item.hidden).SaveX(ctx)
	}

	enabled, hidden := true, false
	total, nodes, err := model.FilterNodeList(ctx, &FilterNodeParams{
		Page: 2, Size: 1, ServerId: []int64{primary.ID}, Protocol: "vless", ProtocolId: "2",
		Enabled: &enabled, IsHidden: &hidden, Preload: true,
	})
	if err != nil || total != 2 || len(nodes) != 1 || nodes[0].Id != 20 {
		t.Fatalf("filtered node page = %v (total %d), %v; want node 20 out of 2", nodes, total, err)
	}
	if server := nodes[0].Server; server == nil || server.Id != primary.ID || server.Address != primary.Address {
		t.Fatalf("node's preloaded server = %+v; want %d at %s", server, primary.ID, primary.Address)
	}

	assertIDs := func(found []*Node, err error, want []int64) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		var got []int64
		for _, item := range found {
			got = append(got, item.Id)
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Fatalf("visible node IDs = %v; want %v", got, want)
		}
	}
	visible, err := model.QueryEnabledVisibleNodes(ctx)
	assertIDs(visible, err, []int64{10, 20, 50, 60})
	visible, err = model.QueryEnabledVisibleNodesByIds(ctx, []int64{20, 30, 40, 999})
	assertIDs(visible, err, []int64{20})
}

func TestNodeUpdateRepairsDuplicateSortPositions(t *testing.T) {
	ctx := t.Context()
	client := deletionClient(t)
	model := NewModel(client, nil)
	for _, id := range []int64{20, 10, 30} {
		client.Node.Create().SetID(id).SetName("original").SetPort(443).SetSort(5).SaveX(ctx)
	}

	edited, err := model.FindOneNode(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	edited.Name, edited.Port = "updated", 8443
	edited.NodeGroupIds = JSONInt64Slice{7, 9}
	if err := model.UpdateNode(ctx, edited); err != nil {
		t.Fatal(err)
	}
	stored := client.Node.GetX(ctx, edited.Id)
	if stored.Name != "updated" || stored.Port != 8443 || stored.Sort != 4 || !slices.Equal(stored.NodeGroupIds, []int64{7, 9}) {
		t.Fatalf("saved node = %+v; want edited fields and a sort position after its peers", stored)
	}
	for id, wantSort := range map[int64]int{20: 2, 30: 3} {
		if peer := client.Node.GetX(ctx, id); peer.Sort != wantSort || peer.Name != "original" || peer.Port != 443 {
			t.Fatalf("reordered peer = %+v; want original fields and sort %d", peer, wantSort)
		}
	}
}

func TestNodeSortUpdateInvalidatesOnlyItsServerCache(t *testing.T) {
	ctx := t.Context()
	client := deletionClient(t)
	cache := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	model := NewModel(client, redisClient)
	client.Node.Create().SetID(10).SetServerID(1).SetSort(1).SaveX(ctx)
	client.Node.Create().SetID(20).SetServerID(2).SetSort(2).SaveX(ctx)

	cacheKeys := func(serverID int64) []string {
		return []string{
			fmt.Sprintf("%s%d", ServerUserListCacheKey, serverID),
			fmt.Sprintf("%s%d:vless:1", ServerUserListCacheKey, serverID),
			fmt.Sprintf("%s%d:vless:1", ServerConfigCacheKey, serverID),
		}
	}
	for _, serverID := range []int64{1, 2} {
		for _, key := range cacheKeys(serverID) {
			if ok, err := PublishServerCache(ctx, redisClient, serverID, key, []byte("cached"), "0"); err != nil || !ok {
				t.Fatalf("publishing %q = %v, %v", key, ok, err)
			}
		}
	}

	if err := model.UpdateNodeSort(ctx, 10, 4); err != nil {
		t.Fatal(err)
	}
	if got := client.Node.GetX(ctx, 10).Sort; got != 4 {
		t.Fatalf("persisted node sort = %d; want 4", got)
	}
	if got := client.Node.GetX(ctx, 20).Sort; got != 2 {
		t.Fatalf("unrelated node sort = %d; want 2", got)
	}
	for _, key := range cacheKeys(1) {
		count, err := redisClient.Exists(ctx, key).Result()
		if err != nil || count != 0 {
			t.Fatalf("updated server cache key %q still exists (count %d, err %v)", key, count, err)
		}
	}
	for _, key := range cacheKeys(2) {
		data, _, err := ReadServerCache(ctx, redisClient, key, "0")
		if err != nil || string(data) != "cached" {
			t.Fatalf("unrelated server cache key %q = %q, %v; want cached", key, data, err)
		}
	}
	key := cacheKeys(1)[0]
	if ok, err := PublishServerCache(ctx, redisClient, 1, key, []byte("stale"), "0"); err != nil || ok {
		t.Fatalf("stale publication after node sort = %v, %v; want rejected", ok, err)
	}
}
