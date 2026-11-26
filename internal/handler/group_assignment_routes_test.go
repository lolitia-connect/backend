package handler

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent"
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

func TestGroupSettingsAndStatusReportTheSameProgress(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, admin := f.account(true)
	check := func(want types.RecalculationState) {
		t.Helper()
		var status types.RecalculationState
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/admin/group/recalculation/status", admin, "", http.StatusOK), &status))
		require.Equal(t, want, status)
		var settings types.GetGroupConfigResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/admin/group/config", admin, "", http.StatusOK), &settings))
		require.Equal(t, status, settings.State)
	}
	check(types.RecalculationState{State: "idle"})
	run := f.client.GroupHistory.Create().SetGroupMode("average").SetTriggerType("manual").SetState("running").
		SetTotalUsers(10).SetSuccessCount(4).SetFailedCount(1).SaveX(ctx)
	check(types.RecalculationState{State: "running", Progress: 5, Total: 10})
	f.client.GroupHistory.UpdateOneID(run.ID).SetState("completed").SetSuccessCount(9).ExecX(ctx)
	check(types.RecalculationState{State: "completed", Progress: 10, Total: 10})
}

func TestSwitchingGroupModesKeepsSavedRulesWhenHistoryIsUnavailable(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	const settingsPath = "/v1/admin/group/config"
	read := func() types.GetGroupConfigResponse {
		t.Helper()
		var settings types.GetGroupConfigResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, settingsPath, operator, "", http.StatusOK), &settings))
		return settings
	}
	f.request(http.MethodPut, settingsPath, operator, `{"enabled":true,"mode":"average","config":{"average_config":{"node_group_count":3}}}`, http.StatusOK)
	f.request(http.MethodPut, settingsPath, operator, `{"enabled":true,"mode":"subscribe","config":{"subscribe_config":{"region":"East"}}}`, http.StatusOK)
	f.request(http.MethodPut, settingsPath, operator, `{"enabled":true,"mode":"traffic","config":{"traffic_config":{"threshold":12}}}`, http.StatusOK)
	rules := map[string]any{
		"average_config":   map[string]any{"node_group_count": float64(3)},
		"subscribe_config": map[string]any{"region": "East"},
		"traffic_config":   map[string]any{"threshold": float64(12)},
	}
	f.client.GroupHistory.Create().SetGroupMode("traffic").SetTriggerType("manual").SetState("running").SetTotalUsers(8).SetSuccessCount(3).SaveX(ctx)
	historyOffline := true
	f.client.GroupHistory.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
			if historyOffline {
				return nil, fmt.Errorf("assignment history unavailable")
			}
			return next.Query(ctx, q)
		})
	}))
	saved := read()
	require.True(t, saved.Enabled)
	require.Equal(t, "traffic", saved.Mode)
	require.Equal(t, rules, saved.Config)
	require.Equal(t, types.RecalculationState{State: "idle"}, saved.State)
	historyOffline = false
	saved = read()
	require.Equal(t, types.RecalculationState{State: "running", Progress: 3, Total: 8}, saved.State)
	require.Equal(t, rules, saved.Config)
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
	for _, group := range groups.List {
		f.client.Node.Create().SetName(group.Name + " gateway").SetNodeGroupIds([]int64{group.Id}).SetEnabled(true).SaveX(ctx)
	}
	for i, group := range groups.List {
		member, _ := f.account(false)
		f.client.UserSubscribe.Create().SetUserID(member.ID).SetOrderID(int64(i + 1)).SetSubscribeID(plan.ID).SetStatus(1).SaveX(ctx)
		want = append(want, []string{fmt.Sprint(member.ID), fmt.Sprint(group.Id), group.Name})
	}
	f.request(http.MethodPost, base+"/recalculate", admin, `{"mode":"average"}`, http.StatusOK)
	var state types.RecalculationState
	get("/recalculation/status", &state)
	require.Equal(t, types.RecalculationState{State: "completed", Progress: 2, Total: 2}, state)
	for _, assignment := range want {
		var preview types.PreviewUserNodesResponse
		get("/preview?user_id="+assignment[0], &preview)
		require.Equal(t, assignment[0], fmt.Sprint(preview.UserId))
		require.Len(t, preview.NodeGroups, 1)
		region := preview.NodeGroups[0]
		require.Equal(t, assignment[1], fmt.Sprint(region.Id))
		require.Equal(t, assignment[2], region.Name)
		require.Len(t, region.Nodes, 1)
		require.Equal(t, region.Name+" gateway", region.Nodes[0].Name)
	}
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
