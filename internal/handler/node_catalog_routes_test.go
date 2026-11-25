package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestSubscriberCatalogFollowsPublishedNodesAndExpiryGroup(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	customer, session := f.account(false)
	const admin = "/v1/admin/server"
	const catalog = "/v1/public/subscribe/node/list"
	f.request(http.MethodPost, admin+"/create", operator, `{"name":"Tokyo","country":"JP","city":"Tokyo","address":"192.0.2.5","protocols":[{"id":"primary","type":"vless","enable":true,"port":443}]}`, http.StatusOK)
	var servers types.FilterServerListResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, admin+"/list?page=1&size=10", operator, "", http.StatusOK), &servers))
	require.Len(t, servers.List, 1)
	server := servers.List[0]
	grace := f.client.NodeGroup.Create().SetName("Grace period").SetType(group.NodeGroupTypeApp).SetIsExpiredGroup(true).SetExpiredDaysLimit(2).SetMaxTrafficGBExpired(1).SaveX(ctx)
	createNode := func(name, tag string, nodeGroup *ent.NodeGroup) {
		t.Helper()
		groups := `[]`
		if nodeGroup != nil {
			groups = fmt.Sprintf(`["%d"]`, nodeGroup.ID)
		}
		body := fmt.Sprintf(`{"name":%q,"server_id":"%d","protocol":"vless","protocol_id":"primary","address":%q,"port":443,"tags":[%q],"enabled":true,"node_group_ids":%s}`, name, server.Id, name+".example.test", tag, groups)
		f.request(http.MethodPost, admin+"/node/create", operator, body, http.StatusOK)
	}
	createNode("direct", "region_1", nil)
	createNode("regional", "region_1", nil)
	createNode("unrelated", "regionX1", nil)
	createNode("fallback", "grace", grace)
	var nodes types.FilterNodeListResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, admin+"/node/list?page=1&size=10", operator, "", http.StatusOK), &nodes))
	require.Len(t, nodes.List, 4)
	ids := make(map[string]int64)
	for _, n := range nodes.List {
		ids[n.Name] = n.Id
	}
	plan := f.client.Subscribe.Create().SetName("Regional").SetNodes(fmt.Sprint(ids["direct"])).SetNodeTags("region_1").SaveX(ctx)
	const credential = "2a8bed53-c140-4be8-ad02-c27b58d3ba22"
	sub := f.client.UserSubscribe.Create().SetUserID(customer.ID).SetOrderID(1).SetSubscribeID(plan.ID).SetStatus(1).SetToken("regional-fixture").SetUUID(credential).SetExpireTime(time.Now().Add(time.Hour)).SaveX(ctx)
	assertVisible := func(names ...string) {
		t.Helper()
		var result types.QueryUserSubscribeNodeListResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, catalog, session, "", http.StatusOK), &result))
		require.Len(t, result.List, 1)
		require.Equal(t, sub.ID, result.List[0].Id)
		var got []string
		for _, node := range result.List[0].Nodes {
			require.Equal(t, ids[node.Name], node.Id)
			require.Equal(t, node.Name+".example.test", node.Address)
			require.Equal(t, credential, node.Uuid)
			got = append(got, node.Name)
		}
		require.ElementsMatch(t, names, got)
	}
	assertVisible("direct", "regional")
	f.request(http.MethodPost, admin+"/node/status/toggle", operator, fmt.Sprintf(`{"id":"%d","enable":false}`, ids["direct"]), http.StatusOK)
	assertVisible("regional")
	expiredAt := time.Now().Add(-24 * time.Hour)
	f.client.UserSubscribe.UpdateOneID(sub.ID).SetStatus(2).SetExpireTime(expiredAt).SetFinishedAt(expiredAt).SaveX(ctx)
	assertVisible("fallback")
	f.client.UserSubscribe.UpdateOneID(sub.ID).SetExpiredDownload(1 << 30).SaveX(ctx)
	f.request(http.MethodGet, catalog, session, "", xerr.SubscribeExpired)
}
