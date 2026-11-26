package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	framework "entgo.io/ent"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/device"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestMemberCatalogUsesDefaultLanguageOnlyWhenLocalizedPlansAreUnavailable(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, member := f.account(false)
	fallback := f.client.Subscribe.Create().SetName("Monthly").SetLanguage("").SetSell(true).SetUnitPrice(500).SaveX(ctx)
	localized := f.client.Subscribe.Create().SetName("Mensuel").SetLanguage("fr").SetSell(true).SetUnitPrice(450).
		SetDiscount(`[{"quantity":3,"discount":0.8}]`).SaveX(ctx)
	f.client.Subscribe.Create().SetName("Retired").SetLanguage("fr").SetSell(false).SaveX(ctx)
	path := "/v1/public/subscribe/list?language=fr"
	read := func() types.Subscribe {
		t.Helper()
		var result types.QuerySubscribeListResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path, member, "", http.StatusOK), &result))
		require.EqualValues(t, 1, result.Total)
		require.Len(t, result.List, 1)
		return result.List[0]
	}
	plan := read()
	require.Equal(t, localized.ID, plan.Id)
	require.Equal(t, localized.UnitPrice, plan.UnitPrice)
	require.Equal(t, []types.SubscribeDiscount{{Quantity: 3, Discount: 0.8}}, plan.Discount)
	f.client.Subscribe.UpdateOneID(localized.ID).SetSell(false).ExecX(ctx)
	fallbackOffline := true
	var pageReads int
	f.client.Subscribe.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
			if framework.QueryFromContext(ctx).Op == framework.OpQueryAll {
				pageReads++
				if fallbackOffline && pageReads == 2 {
					return nil, errors.New("default catalog temporarily unavailable")
				}
			}
			return next.Query(ctx, q)
		})
	}))
	require.Empty(t, f.request(http.MethodGet, path, member, "", xerr.DatabaseQueryError))
	fallbackOffline = false
	plan = read()
	require.Equal(t, fallback.ID, plan.Id)
	require.Equal(t, fallback.UnitPrice, plan.UnitPrice)
	require.Empty(t, plan.Discount)
}

func TestPlanGroupCatalogKeepsLastCommittedEdition(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	const editor = "/v1/admin/subscribe/group"
	list := func() map[int64]string {
		t.Helper()
		var page types.GetSubscribeGroupListResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, editor+"/list", operator, "", http.StatusOK), &page))
		require.EqualValues(t, len(page.List), page.Total)
		groups := make(map[int64]string)
		for _, g := range page.List {
			groups[g.Id] = g.Name + ": " + g.Description
		}
		return groups
	}
	var readOnly bool
	f.client.SubscribeGroup.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if readOnly {
				return nil, errors.New("catalog storage is read-only")
			}
			return next.Mutate(ctx, m)
		})
	})
	const draft = `{"name":"Monthly","description":"Billed monthly"}`
	readOnly = true
	f.request(http.MethodPost, editor, operator, draft, xerr.DatabaseInsertError)
	require.Empty(t, list())
	readOnly = false
	f.request(http.MethodPost, editor, operator, draft, http.StatusOK)
	monthly := f.client.SubscribeGroup.Query().OnlyX(ctx).ID
	annual := f.client.SubscribeGroup.Create().SetName("Yearly").SetDescription("Billed yearly").SaveX(ctx).ID
	keep := f.client.SubscribeGroup.Create().SetName("Special").SetDescription("Gift plans").SaveX(ctx).ID
	original := map[int64]string{monthly: "Monthly: Billed monthly", annual: "Yearly: Billed yearly", keep: "Special: Gift plans"}
	require.Equal(t, original, list())
	edit := fmt.Sprintf(`{"id":"%d","name":"Legacy","description":"Closed to new customers"}`, monthly)
	single := fmt.Sprintf(`{"id":"%d"}`, monthly)
	selection := fmt.Sprintf(`{"ids":["%d","%d"]}`, monthly, annual)
	readOnly = true
	f.request(http.MethodPut, editor, operator, edit, xerr.DatabaseUpdateError)
	f.request(http.MethodDelete, editor, operator, single, xerr.DatabaseDeletedError)
	f.request(http.MethodDelete, editor+"/batch", operator, selection, xerr.DatabaseDeletedError)
	require.Equal(t, original, list())
	readOnly = false
	f.request(http.MethodPut, editor, operator, edit, http.StatusOK)
	original[monthly] = "Legacy: Closed to new customers"
	require.Equal(t, original, list())
	f.request(http.MethodDelete, editor+"/batch", operator, selection, http.StatusOK)
	f.request(http.MethodDelete, editor, operator, single, http.StatusOK)
	require.Equal(t, map[int64]string{keep: "Special: Gift plans"}, list())
}

func TestPlanEditorPreservesExactNodeAssignmentsAcrossCatalogViews(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	f.service.DeviceManager = device.NewDeviceManager(60, 30)
	t.Cleanup(f.service.DeviceManager.Close)
	const base = "/v1/admin/subscribe"
	for id, name := range map[int64]string{17: "Local", 9007199254740992: "West", 9007199254740993: "East"} {
		f.client.Node.Create().SetID(id).SetName(name).SaveX(ctx)
	}
	const original = `{"name":"Regional pass","unit_price":750,"unit_time":"Month","nodes":[17,"9007199254740993"]}`
	f.request(http.MethodPost, base+"/", operator, original, http.StatusOK)
	plan := f.client.Subscribe.Query().OnlyX(ctx)
	type selection struct {
		Nodes []string `json:"nodes"`
	}
	check := func(want ...string) {
		t.Helper()
		var detail selection
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, fmt.Sprintf("%s/details?id=%d", base, plan.ID), operator, "", http.StatusOK), &detail))
		var page struct {
			List []selection `json:"list"`
		}
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, base+"/list?page=1&size=10", operator, "", http.StatusOK), &page))
		require.Len(t, page.List, 1)
		require.Equal(t, want, detail.Nodes)
		require.Equal(t, want, page.List[0].Nodes)
	}
	check("17", "9007199254740993")
	edit := func(nodes string, code uint32) {
		t.Helper()
		body := fmt.Sprintf(`{"id":"%d","name":"Regional pass","unit_price":750,"unit_time":"Month","nodes":%s}`, plan.ID, nodes)
		f.request(http.MethodPut, base+"/", operator, body, code)
	}
	for _, invalid := range []string{`[17,"9223372036854775808"]`, `[17,false]`, `"17"`} {
		edit(invalid, xerr.InvalidParams)
		check("17", "9007199254740993")
	}
	edit(`[9007199254740992]`, http.StatusOK)
	check("9007199254740992")
	edit(`null`, http.StatusOK)
	check([]string{}...)
}

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
	var listUnavailable bool
	f.client.UserSubscribe.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
			if listUnavailable {
				return nil, errors.New("subscriber list unavailable")
			}
			return next.Query(ctx, q)
		})
	}))
	single := fmt.Sprintf(`{"id":"%d"}`, basic.Id)
	batch := fmt.Sprintf(`{"ids":["%d","%d"]}`, plus.Id, basic.Id)
	listUnavailable = true
	f.request(http.MethodDelete, plans+"/", operator, single, xerr.DatabaseQueryError)
	f.request(http.MethodDelete, plans+"/batch", operator, batch, xerr.DatabaseDeletedError)
	require.Equal(t, visible, catalog())
	listUnavailable = false
	f.request(http.MethodDelete, plans+"/", operator, single, xerr.SubscribeIsUsedError)
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
