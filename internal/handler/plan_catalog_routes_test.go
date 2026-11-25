package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/device"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestPublishedPlanOrderAndBatchRetirement(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	customer, _ := f.account(false)
	f.service.DeviceManager = device.NewDeviceManager(60, 30)
	t.Cleanup(f.service.DeviceManager.Close)
	const plans = "/v1/admin/subscribe"
	const groups = "/v1/admin/group"
	f.request(http.MethodPost, groups+"/node", operator, `{"name":"Regional service","type":"subscribe"}`, http.StatusOK)
	var available types.GetNodeGroupListResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, groups+"/node/list?page=1&size=10", operator, "", http.StatusOK), &available))
	require.Len(t, available.List, 1)
	group := available.List[0]
	for _, entry := range []struct {
		name string
		show bool
	}{{"Basic", true}, {"Draft", false}, {"Plus", true}} {
		f.request(http.MethodPost, plans+"/", operator, fmt.Sprintf(`{"name":%q,"language":"en","show":%t,"sell":%t,"node_group_ids":["%d"],"unit_price":1000,"unit_time":"Month"}`, entry.name, entry.show, entry.show, group.Id), http.StatusOK)
	}
	var mapping types.GetSubscribeGroupMappingResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, groups+"/subscribe/mapping", operator, "", http.StatusOK), &mapping))
	require.ElementsMatch(t, []types.SubscribeGroupMappingItem{{SubscribeName: "Basic", NodeGroupName: group.Name}, {SubscribeName: "Draft", NodeGroupName: group.Name}, {SubscribeName: "Plus", NodeGroupName: group.Name}}, mapping.List)
	list := func() []types.SubscribeItem {
		t.Helper()
		var result types.GetSubscribeListResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, plans+"/list?page=1&size=10&language=en", operator, "", http.StatusOK), &result))
		return result.List
	}
	catalog := func() []types.Subscribe {
		t.Helper()
		var result types.GetSubscriptionResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/portal/subscribe?language=en", "", "", http.StatusOK), &result))
		return result.List
	}
	initial := list()
	require.Len(t, initial, 3)
	basic, draft, plus := initial[0], initial[1], initial[2]
	require.Equal(t, "Draft", draft.Name)
	beforeSort := catalog()
	sortRequest := fmt.Sprintf(`{"sort":[{"id":"%d","sort":1},{"id":"%d","sort":2}]}`, plus.Id, basic.Id)
	var sortWrites int
	f.client.Subscribe.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if _, changesRank := mutation.(*ent.SubscribeMutation).Sort(); changesRank {
				sortWrites++
				if sortWrites == 2 {
					return nil, errors.New("rank write interrupted")
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
	f.request(http.MethodPost, plans+"/sort", operator, sortRequest, xerr.DatabaseUpdateError)
	require.Equal(t, 2, sortWrites)
	require.Equal(t, initial, list())
	require.Equal(t, beforeSort, catalog())
	f.request(http.MethodPost, plans+"/sort", operator, sortRequest, http.StatusOK)
	ordered := list()
	require.Len(t, ordered, 3)
	require.Equal(t, draft.Id, ordered[1].Id)
	require.Equal(t, plus.Id, ordered[0].Id)
	require.Equal(t, basic.Id, ordered[2].Id)
	visible := catalog()
	require.Len(t, visible, 2)
	require.Equal(t, "Plus", visible[0].Name)
	require.Equal(t, "Basic", visible[1].Name)

	sub := f.client.UserSubscribe.Create().SetUserID(customer.ID).SetSubscribeID(basic.Id).SetOrderID(1).SetStatus(1).SaveX(ctx)
	batch := fmt.Sprintf(`{"ids":["%d","%d"]}`, plus.Id, basic.Id)
	f.request(http.MethodDelete, plans+"/batch", operator, batch, xerr.SubscribeIsUsedError)
	require.Equal(t, visible, catalog())
	f.request(http.MethodPut, plans+"/", operator, fmt.Sprintf(`{"id":"%d","name":"Basic legacy","language":"en","show":false,"sell":false,"sort":%d}`, basic.Id, ordered[2].Sort), http.StatusOK)
	var legacy types.Subscribe
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, fmt.Sprintf("%s/details?id=%d", plans, basic.Id), operator, "", http.StatusOK), &legacy))
	require.Equal(t, "Basic legacy", legacy.Name)
	require.False(t, legacy.Sell)
	visible = catalog()
	require.Len(t, visible, 1)
	require.Equal(t, plus.Id, visible[0].Id)
	f.client.UserSubscribe.UpdateOneID(sub.ID).SetStatus(3).SaveX(ctx)
	f.request(http.MethodDelete, plans+"/batch", operator, batch, http.StatusOK)
	require.Empty(t, catalog())
	remaining := list()
	require.Len(t, remaining, 1)
	require.Equal(t, draft.Id, remaining[0].Id)
}
