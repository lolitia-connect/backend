package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestOperatorCanRetryOneFailedForwardingEntry(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, admin := f.account(true)
	_, member := f.account(false)
	west := f.client.Node.Create().SetName("West").SetAddress("old-west.example.test").SetPort(1000).SaveX(ctx)
	east := f.client.Node.Create().SetName("East").SetAddress("old-east.example.test").SetPort(1001).SaveX(ctx)
	var revision atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/l4/services", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"id":"west","enabled":true},{"id":"east","enabled":true}]`)
	})
	mux.HandleFunc("GET /api/v1/l4/services/{id}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"id":%q,"enabled":true}`, r.PathValue("id"))
	})
	mux.HandleFunc("GET /api/v1/l4/sessions", func(w http.ResponseWriter, r *http.Request) {
		version := revision.Load()
		eastPort := 2400 + version
		if version == 1 {
			eastPort = 0
		}
		fmt.Fprintf(w, `{"rows":[{"sessionId":"w1","serviceId":"west","nodeHost":"west.example.test","allocatedPort":%d,"status":"active"},{"sessionId":"e1","serviceId":"east","nodeHost":"east.example.test","allocatedPort":%d,"status":"active"}]}`, 3400+version, eastPort)
	})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer nkp_local" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(provider.Close)
	const base = "/v1/admin/server/nekoproxy"
	f.request(http.MethodGet, base+"/config", member, "", xerr.InvalidAccess)
	f.request(http.MethodPut, base+"/config", admin, fmt.Sprintf(`{"api_url":%q,"api_token":"nkp_local"}`, provider.URL), http.StatusOK)
	require.JSONEq(t, fmt.Sprintf(`{"api_url":%q,"has_api_token":true}`, provider.URL), string(f.request(http.MethodGet, base+"/config", admin, "", http.StatusOK)))
	bind := func(id int64, forward, session string) {
		t.Helper()
		f.request(http.MethodPut, base+"/binding", admin, fmt.Sprintf(`{"node_id":"%d","forward_id":%q,"session_id":%q}`, id, forward, session), http.StatusOK)
	}
	bind(west.ID, "west", "w1")
	bind(east.ID, "east", "e1")
	entry := func(id int64) types.NekoproxyNodeEntry {
		t.Helper()
		var result types.NekoproxyNodeEntry
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, fmt.Sprintf("%s/binding?node_id=%d", base, id), admin, "", http.StatusOK), &result))
		return result
	}
	syncEntries := func(body string) types.NekoproxySyncResponse {
		t.Helper()
		var result types.NekoproxySyncResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodPost, base+"/sync", admin, body, http.StatusOK), &result))
		return result
	}
	revision.Store(1)
	partial := syncEntries(`{}`)
	require.Equal(t, 1, partial.Updated)
	require.Len(t, partial.Errors, 1)
	require.Equal(t, east.ID, partial.Errors[0].NodeID)
	require.NotEmpty(t, partial.Errors[0].Message)
	require.EqualValues(t, 3401, entry(west.ID).Port)
	original := entry(east.ID)
	require.Equal(t, "east.example.test", original.Address)
	require.EqualValues(t, 2400, original.Port)
	require.NotNil(t, original.Binding)
	require.Equal(t, "e1", original.Binding.SessionID)
	revision.Store(2)
	eastFilter := fmt.Sprintf(`{"node_id":"%d"}`, east.ID)
	retried := syncEntries(eastFilter)
	require.Equal(t, 1, retried.Updated)
	require.Empty(t, retried.Errors)
	require.EqualValues(t, 3401, entry(west.ID).Port)
	require.EqualValues(t, 2402, entry(east.ID).Port)
	f.request(http.MethodDelete, base+"/binding", admin, eastFilter, http.StatusOK)
	require.Zero(t, syncEntries(eastFilter).Updated)
	detached := entry(east.ID)
	require.Nil(t, detached.Binding)
	require.EqualValues(t, 2402, detached.Port)
}

func TestSubscriberMapFollowsTheServerAddress(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	member, session := f.account(false)
	const first, second, unknown = "192.0.2.10", "198.51.100.10", "203.0.113.10"
	var unavailable bool
	transport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = transport })
	http.DefaultTransport = providerTransport(func(req *http.Request) (*http.Response, error) {
		var body string
		switch {
		case unavailable:
			return nil, errors.New("location service unavailable")
		case strings.Contains(req.URL.Path, first):
			body = `{"country_name":"Japan","city":"Osaka","loc":"34.69,135.50"}`
		case strings.Contains(req.URL.Path, second):
			body = `{"country":"GB","city":"Manchester","latitude":"53.48","longitude":"-2.24"}`
		default:
			return nil, fmt.Errorf("unexpected lookup %s", req.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	const base = "/v1/admin/server"
	const fields = `"name":"Regional gateway","protocols":[{"id":"main","type":"vless","enable":true,"port":443}]`
	f.request(http.MethodPost, base+"/create", operator, fmt.Sprintf(`{%s,"address":%q}`, fields, first), http.StatusOK)
	server := f.client.Server.Query().OnlyX(ctx)
	edge := f.client.Node.Create().SetServerID(server.ID).SetName("Member gateway").SetProtocol("vless").SetProtocolID("main").SetPort(443).SetAddress("entry.example.test").SetEnabled(true).SaveX(ctx)
	plan := f.client.Subscribe.Create().SetName("Regional pass").SetNodes(fmt.Sprint(edge.ID)).SaveX(ctx)
	f.client.UserSubscribe.Create().SetUserID(member.ID).SetOrderID(1).SetSubscribeID(plan.ID).SetStatus(1).SetUUID("2a8bed53-c140-4be8-ad02-c27b58d3ba22").SetExpireTime(time.Now().Add(time.Hour)).SaveX(ctx)
	location := func() [6]string {
		t.Helper()
		var page types.QueryUserSubscribeNodeListResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/subscribe/node/list", session, "", http.StatusOK), &page))
		require.Len(t, page.List, 1)
		require.Len(t, page.List[0].Nodes, 1)
		n := page.List[0].Nodes[0]
		require.Equal(t, edge.ID, n.Id)
		return [6]string{n.Country, n.City, n.Latitude, n.Longitude, n.LatitudeCenter, n.LongitudeCenter}
	}
	move := func(address string) {
		t.Helper()
		f.request(http.MethodPost, base+"/update", operator, fmt.Sprintf(`{%s,"id":"%d","address":%q}`, fields, server.ID, address), http.StatusOK)
		require.Equal(t, address, f.client.Server.GetX(ctx, server.ID).Address)
	}
	require.Equal(t, [6]string{"Japan", "Osaka", "34.69", "135.50", "35.6762", "139.6503"}, location())
	move(second)
	manchester := [6]string{"GB", "Manchester", "53.48", "-2.24", "51.5074", "-0.1278"}
	require.Equal(t, manchester, location())
	unavailable = true
	move(second)
	require.Equal(t, manchester, location())
	move(unknown)
	require.Equal(t, [6]string{}, location(), "the previous address's map marker must not follow the server")
}

func TestServerDashboardReconcilesProtocolReportsWithSubscriptionUsage(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	owner, _ := f.account(false)
	plan := f.client.Subscribe.Create().SetName("Travel").SaveX(ctx)
	expires := time.Now().AddDate(0, 1, 0).Truncate(time.Second)
	sub := f.client.UserSubscribe.Create().SetUserID(owner.ID).SetOrderID(1).SetSubscribeID(plan.ID).
		SetExpireTime(expires).SetUpload(120).SetDownload(380).SaveX(ctx)
	const editor = "/v1/admin/server"
	const config = `{"name":"West","address":"192.0.2.7","country":"GB","protocols":[{"id":"a","type":"vless","port":443,"enable":true},{"id":"b","type":"vmess","port":8443,"enable":true}]}`
	f.request(http.MethodPost, editor+"/create", operator, config, http.StatusOK)
	edge := f.client.Server.Query().OnlyX(ctx)
	store := f.service.Store.Node()
	require.NoError(t, store.UpdateStatusCache(ctx, edge.ID, &node.Status{Cpu: 0.2, Mem: 0.6, Disk: 0.4}))
	require.NoError(t, store.UpdateOnlineUserSubscribe(ctx, edge.ID, "vless:a", node.OnlineUserSubscribe{sub.ID: {"192.0.2.12"}, sub.ID + 1: {"192.0.2.99"}}))
	require.NoError(t, store.UpdateOnlineUserSubscribe(ctx, edge.ID, "vmess:b", node.OnlineUserSubscribe{sub.ID: {"2001:db8::12"}}))
	for _, heartbeat := range []struct {
		age    time.Duration
		status string
	}{{6 * time.Minute, "offline"}, {4 * time.Minute, "warning"}, {0, "online"}} {
		require.NoError(t, store.UpdateServerLastReportedAt(ctx, edge.ID, time.Now().Add(-heartbeat.age)))
		var page types.FilterServerListResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, editor+"/list?search=West", operator, "", http.StatusOK), &page))
		require.Len(t, page.List, 1)
		status := page.List[0].Status
		require.Equal(t, heartbeat.status, status.Status)
		require.Equal(t, [3]float64{0.2, 0.6, 0.4}, [3]float64{status.Cpu, status.Mem, status.Disk})
		require.Len(t, status.Online, 1)
		user := status.Online[0]
		require.Equal(t, owner.ID, user.UserId)
		require.Equal(t, sub.ID, user.SubscribeId)
		require.Equal(t, plan.Name, user.Subscribe)
		require.Equal(t, expires.UnixMilli(), user.ExpiredAt)
		require.EqualValues(t, 500, user.Traffic)
		require.ElementsMatch(t, []types.ServerOnlineIP{{IP: "192.0.2.12", Protocol: "vless"}, {IP: "2001:db8::12", Protocol: "vmess"}}, user.IP)
	}
}

func TestNodeCatalogRetainsLastEditionAcrossFailedWrites(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	const base = "/v1/admin/server"
	var blocked string
	f.client.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if m.Type() == blocked {
				return nil, errors.New("catalog is read-only")
			}
			return next.Mutate(ctx, m)
		})
	})
	serverForm := `{"name":"Original server","address":"192.0.2.10","country":"GB","protocols":[{"id":"west","type":"vless","enable":true,"port":443}]}`
	post := func(path, form string, want uint32) {
		t.Helper()
		f.request(http.MethodPost, base+path, operator, form, want)
	}
	blocked = ent.TypeServer
	post("/create", serverForm, xerr.DatabaseInsertError)
	require.Zero(t, f.client.Server.Query().CountX(ctx))
	blocked = ""
	post("/create", serverForm, http.StatusOK)
	server := f.client.Server.Query().OnlyX(ctx)
	serverID := fmt.Sprintf(`{"id":"%d"}`, server.ID)
	blockUpdate := fmt.Sprintf(`{"id":"%d","name":"Changed","address":"192.0.2.10","country":"GB","protocols":[{"id":"west","type":"vless","enable":true,"port":8443}]}`, server.ID)
	blocked = ent.TypeServer
	post("/update", blockUpdate, xerr.DatabaseUpdateError)
	var published types.GetServerProtocolsResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, fmt.Sprintf("%s/protocols?id=%d", base, server.ID), operator, "", http.StatusOK), &published))
	require.Len(t, published.Protocols, 1)
	require.EqualValues(t, 443, published.Protocols[0].Port)
	require.Equal(t, "Original server", f.client.Server.GetX(ctx, server.ID).Name)
	blocked = ""
	post("/update", blockUpdate, http.StatusOK)
	nodeForm := fmt.Sprintf(`{"name":"West entry","server_id":"%d","protocol":"vless","protocol_id":"west","enabled":true,"address":"entry.example.test","port":8443}`, server.ID)
	blocked = ent.TypeNode
	post("/node/create", nodeForm, xerr.DatabaseInsertError)
	require.Zero(t, f.client.Node.Query().CountX(ctx))
	blocked = ""
	post("/node/create", nodeForm, http.StatusOK)
	entry := f.client.Node.Query().OnlyX(ctx)
	nodeID := fmt.Sprintf(`{"id":"%d"}`, entry.ID)
	blocked = ent.TypeNode
	post("/node/update", fmt.Sprintf(`{"id":"%d","server_id":"%d","name":"Replacement","port":10443}`, entry.ID, server.ID), xerr.DatabaseUpdateError)
	post("/node/status/toggle", fmt.Sprintf(`{"id":"%d","enable":false}`, entry.ID), xerr.DatabaseUpdateError)
	post("/node/delete", nodeID, xerr.DatabaseDeletedError)
	blocked = ent.TypeServer
	post("/delete", serverID, xerr.DatabaseDeletedError)
	var catalog types.FilterNodeListResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, base+"/node/list", operator, "", http.StatusOK), &catalog))
	require.Len(t, catalog.List, 1)
	require.Equal(t, entry.Name, catalog.List[0].Name)
	require.Equal(t, entry.Address, catalog.List[0].Address)
	require.Equal(t, entry.Port, catalog.List[0].Port)
	require.True(t, *catalog.List[0].Enabled)
	blocked = ""
	post("/delete", serverID, http.StatusOK)
	require.Zero(t, f.client.Node.Query().CountX(ctx))
	require.Zero(t, f.client.Server.Query().CountX(ctx))
}

func TestSortingAFilteredCatalogPreservesHiddenSlotsAndRollsBackFailedWrites(t *testing.T) {
	for _, kind := range []string{"server", "node"} {
		t.Run(kind, func(t *testing.T) {
			f := newRouteFixture(t)
			ctx := t.Context()
			_, operator := f.account(true)
			const base = "/v1/admin/server"
			listPath := base + "/list"
			var parent int64
			if kind == "node" {
				parent = f.client.Server.Create().SetName("Regional host").SetProtocols(`[]`).SaveX(ctx).ID
				listPath = base + "/node/list"
			}
			var ids []int64
			for rank, name := range []string{"Edge 10", "Spare", "Edge 2"} {
				if kind == "server" {
					ids = append(ids, f.client.Server.Create().SetName(name).SetSort(rank).SetProtocols(`[]`).SaveX(ctx).ID)
				} else {
					ids = append(ids, f.client.Node.Create().SetName(name).SetServerID(parent).SetSort(rank).SaveX(ctx).ID)
				}
			}
			check := func(filter string, want ...int64) {
				t.Helper()
				var page struct {
					Total int `json:"total"`
					List  []struct {
						ID int64 `json:"id,string"`
					} `json:"list"`
				}
				require.NoError(t, json.Unmarshal(f.request(http.MethodGet, listPath+"?page=1&size=10"+filter, operator, "", http.StatusOK), &page))
				require.Equal(t, len(want), page.Total)
				var got []int64
				for _, item := range page.List {
					got = append(got, item.ID)
				}
				require.Equal(t, want, got)
			}
			sortPath := base + "/" + kind + "/sort"
			selection := func(first, last int64) string {
				return fmt.Sprintf(`{"sort":[{"id":"%d","sort":0},{"id":"%d","sort":1}]}`, first, last)
			}
			check("&search=Edge", ids[0], ids[2])
			f.request(http.MethodPost, sortPath, operator, selection(ids[2], ids[2]+100), xerr.InvalidParams)
			check("", ids...)
			writes := 0
			interrupt := func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
					writes++
					if writes == 2 {
						return nil, errors.New("rank update interrupted")
					}
					return next.Mutate(ctx, m)
				})
			}
			if kind == "server" {
				f.client.Server.Use(interrupt)
			} else {
				f.client.Node.Use(interrupt)
			}
			f.request(http.MethodPost, sortPath, operator, selection(ids[2], ids[0]), xerr.DatabaseUpdateError)
			check("", ids...)
			f.request(http.MethodPost, sortPath, operator, selection(ids[2], ids[0]), http.StatusOK)
			check("", ids[2], ids[1], ids[0])
			f.request(http.MethodPost, sortPath+"-by-name", operator, "", http.StatusOK)
			check("", ids[2], ids[0], ids[1])
		})
	}
}

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
	var choices types.QueryNodeTagResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, admin+"/node/tags", operator, "", http.StatusOK), &choices))
	require.ElementsMatch(t, []string{"region_1", "regionX1", "grace"}, choices.Tags)
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
