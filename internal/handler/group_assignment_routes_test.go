package handler

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestOperatorCanTransferTheExpiredGroupAfterMovingItsNodes(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, admin := f.account(true)
	const path = "/v1/admin/group/node"
	list := func() map[string]types.NodeGroup {
		t.Helper()
		var response types.GetNodeGroupListResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path+"/list?page=1&size=10", admin, "", http.StatusOK), &response))
		groups := make(map[string]types.NodeGroup)
		for _, g := range response.List {
			groups[g.Name] = g
		}
		return groups
	}
	f.request(http.MethodPost, path, admin, `{"name":"Retiring","is_expired_group":true,"for_calculation":true}`, http.StatusOK)
	f.request(http.MethodPost, path, admin, `{"name":"Next region","for_calculation":true}`, http.StatusOK)
	groups := list()
	old, next := groups["Retiring"], groups["Next region"]
	require.Equal(t, 7, old.ExpiredDaysLimit)
	require.False(t, old.ForCalculation)
	request := func(method string, id int64, fields string, want uint32) {
		t.Helper()
		f.request(method, path, admin, fmt.Sprintf(`{"id":"%d"%s}`, id, fields), want)
	}
	const promote = `,"name":"Grace","description":"Post-expiry access","type":"app","sort":2,"is_expired_group":true,"for_calculation":true,"expired_days_limit":3,"max_traffic_gb_expired":5,"speed_limit":8`
	request(http.MethodPut, next.Id, promote, xerr.ERROR)
	require.Equal(t, groups, list())
	plan := f.client.Subscribe.Create().SetName("Default region").SetNodeGroupID(next.Id).SaveX(ctx)
	request(http.MethodPut, old.Id, `,"is_expired_group":false`, http.StatusOK)
	request(http.MethodPut, next.Id, promote, xerr.ERROR)
	require.Equal(t, next, list()[next.Name])
	f.client.Subscribe.UpdateOneID(plan.ID).SetNodeGroupID(0).ExecX(ctx)
	request(http.MethodPut, next.Id, promote, http.StatusOK)
	grace := list()["Grace"]
	require.Equal(t, next.Id, grace.Id)
	require.True(t, grace.IsExpiredGroup)
	require.False(t, grace.ForCalculation)
	require.Equal(t, "app", grace.Type)
	require.Equal(t, "Post-expiry access", grace.Description)
	require.Equal(t, 3, grace.ExpiredDaysLimit)
	require.EqualValues(t, 5, grace.MaxTrafficGBExpired)
	require.Equal(t, 8, grace.SpeedLimit)
	relay := f.client.Node.Create().SetName("Shared relay").SetNodeGroupIds([]int64{old.Id}).SaveX(ctx)
	request(http.MethodDelete, old.Id, "", xerr.ERROR)
	require.EqualValues(t, 1, list()[old.Name].NodeCount)
	f.client.Node.UpdateOneID(relay.ID).SetNodeGroupIds([]int64{next.Id}).ExecX(ctx)
	request(http.MethodDelete, old.Id, "", http.StatusOK)
	remaining := list()
	require.Len(t, remaining, 1)
	require.EqualValues(t, 1, remaining["Grace"].NodeCount)
}

func TestGroupRecalculationProducesDownloadableAssignmentHistory(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, admin := f.account(true)
	const base = "/v1/admin/group"
	get := func(path string, dest any) {
		t.Helper()
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, base+path, admin, "", http.StatusOK), dest))
	}
	f.request(http.MethodPut, base+"/config", admin, `{"enabled":true,"mode":"average","config":{"average_config":{"region":"all"}}}`, http.StatusOK)
	for i, name := range []string{"East, metro", "West"} {
		f.request(http.MethodPost, base+"/node", admin, fmt.Sprintf(`{"name":%q,"sort":%d,"for_calculation":true}`, name, i+1), http.StatusOK)
	}
	var groups types.GetNodeGroupListResponse
	get("/node/list?page=1&size=10", &groups)
	require.Len(t, groups.List, 2)
	east, west := groups.List[0], groups.List[1]
	require.Equal(t, "East, metro", east.Name)
	require.Equal(t, "West", west.Name)
	plan := f.client.Subscribe.Create().SetName("Two regions").SetNodeGroupIds([]int64{east.Id, west.Id}).SaveX(ctx)
	var mapping types.GetSubscribeGroupMappingResponse
	get("/subscribe/mapping", &mapping)
	require.Equal(t, []types.SubscribeGroupMappingItem{{SubscribeName: plan.Name, NodeGroupName: east.Name}}, mapping.List)
	var want [][]string
	for i, group := range groups.List {
		member, _ := f.account(false)
		f.client.UserSubscribe.Create().SetUserID(member.ID).SetOrderID(int64(i + 1)).SetSubscribeID(plan.ID).SetStatus(1).SaveX(ctx)
		want = append(want, []string{fmt.Sprint(member.ID), fmt.Sprint(group.Id), group.Name})
	}
	f.request(http.MethodPost, base+"/recalculate", admin, `{"mode":"average"}`, http.StatusOK)
	var state types.RecalculationState
	get("/recalculation/status", &state)
	require.Equal(t, types.RecalculationState{State: "completed", Progress: 2, Total: 2}, state)
	var history types.GetGroupHistoryResponse
	get("/history?group_mode=average&trigger_type=manual", &history)
	require.Len(t, history.List, 1)
	id := history.List[0].Id
	var detail types.GetGroupHistoryDetailResponse
	get(fmt.Sprintf("/history/detail?id=%d", id), &detail)
	require.EqualValues(t, 2, detail.SuccessCount)
	require.Equal(t, map[string]any{"region": "all"}, detail.ConfigSnapshot["config"])
	export := func(query, filename string) [][]string {
		t.Helper()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, base+"/export"+query, nil)
		req.Header.Set("Authorization", admin)
		recorder := httptest.NewRecorder()
		f.router.ServeHTTP(recorder, req)
		require.Equal(t, http.StatusOK, recorder.Code)
		require.Contains(t, recorder.Header().Get("Content-Type"), "text/csv")
		disposition, params, err := mime.ParseMediaType(recorder.Header().Get("Content-Disposition"))
		require.NoError(t, err)
		require.Equal(t, "attachment", disposition)
		require.Equal(t, filename, params["filename"])
		rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(recorder.Body.String(), "\ufeff"))).ReadAll()
		require.NoError(t, err)
		require.NotEmpty(t, rows)
		return rows[1:]
	}
	require.ElementsMatch(t, want, export(fmt.Sprintf("?history_id=%d", id), fmt.Sprintf("group_result_%d.csv", id)))
	require.ElementsMatch(t, want, export("", "group_result.csv"))
	f.request(http.MethodPost, base+"/reset", admin, `{"confirm":true}`, http.StatusOK)
	var reset types.GetGroupConfigResponse
	get("/config", &reset)
	require.False(t, reset.Enabled)
	require.Equal(t, "idle", reset.State.State)
	require.Empty(t, f.client.Subscribe.GetX(ctx, plan.ID).NodeGroupIds)
	require.Zero(t, f.client.GroupHistory.Query().CountX(ctx))
	require.Zero(t, f.client.NodeGroup.Query().CountX(ctx))
}
