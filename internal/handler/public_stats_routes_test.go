package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	framework "entgo.io/ent"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/config"
	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestUserDashboardRecoversRegistrationCountsOnNextRequest(t *testing.T) {
	t.Setenv("PPANEL_MODE", "production")
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	buyer, _ := f.account(false)
	f.client.Order.Create().SetOrderNo("dashboard-recharge").SetUserID(buyer.ID).SetMethod("card").SetStatus(order.StatusPaid).
		SetType(order.TypeRecharge).SetAmount(275).SaveX(ctx)
	countsOffline := true
	f.client.User.Intercept(framework.InterceptFunc(func(next framework.Querier) framework.Querier {
		return framework.QuerierFunc(func(ctx context.Context, q framework.Query) (framework.Value, error) {
			if countsOffline && framework.QueryFromContext(ctx).Op == framework.OpQueryCount {
				return nil, errors.New("registration counts temporarily unavailable")
			}
			return next.Query(ctx, q)
		})
	}))
	read := func() *types.UserStatisticsResponse {
		t.Helper()
		var page types.UserStatisticsResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/admin/console/user", operator, "", http.StatusOK), &page))
		return &page
	}
	partial := read()
	require.Zero(t, partial.All.Register)
	require.EqualValues(t, 1, partial.All.RechargeOrderUsers)
	countsOffline = false
	recovered := read()
	require.EqualValues(t, 2, recovered.All.Register)
	require.EqualValues(t, 1, recovered.All.RechargeOrderUsers)
	f.client.User.Create().SetPassword("fixture").SaveX(ctx)
	require.Equal(t, recovered, read())
}

func TestRevenueDashboardRefillsMissingChartsOnNextRequest(t *testing.T) {
	t.Setenv("PPANEL_MODE", "production")
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	buyer, _ := f.account(false)
	paid := f.client.Order.Create().SetOrderNo("dashboard-settled").SetUserID(buyer.ID).SetMethod("card").
		SetStatus(order.StatusPaid).SetType(order.TypeRecharge).SetAmount(275).SaveX(ctx)
	f.client.Order.Create().SetOrderNo("dashboard-pending").SetUserID(buyer.ID).SetMethod("card").
		SetStatus(order.StatusPending).SetType(order.TypeRecharge).SetAmount(500).SaveX(ctx)
	chartsOffline := true
	f.client.Order.Intercept(framework.InterceptFunc(func(next framework.Querier) framework.Querier {
		return framework.QuerierFunc(func(ctx context.Context, q framework.Query) (framework.Value, error) {
			if chartsOffline && slices.Contains(framework.QueryFromContext(ctx).Fields, "created_at") {
				return nil, errors.New("time series temporarily unavailable")
			}
			return next.Query(ctx, q)
		})
	}))
	read := func() types.RevenueStatisticsResponse {
		t.Helper()
		var report types.RevenueStatisticsResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/admin/console/revenue", operator, "", http.StatusOK), &report))
		for _, period := range []types.OrdersStatistics{report.Today, report.Monthly, report.All} {
			require.EqualValues(t, 275, period.AmountTotal)
			require.EqualValues(t, 275, period.RechargeOrderAmount)
		}
		return report
	}
	partial := read()
	require.Empty(t, partial.Monthly.List)
	require.Empty(t, partial.All.List)
	chartsOffline = false
	recovered := read()
	for _, chart := range []struct {
		rows []types.OrdersStatistics
		date string
	}{{recovered.Monthly.List, paid.CreatedAt.Format(time.DateOnly)}, {recovered.All.List, paid.CreatedAt.Format("2006-01")}} {
		require.Len(t, chart.rows, 1)
		require.Equal(t, chart.date, chart.rows[0].Date)
		require.EqualValues(t, 275, chart.rows[0].RechargeOrderAmount)
	}
	chartsOffline = true
	require.Equal(t, recovered, read())
}

func TestOperatorCanDrillDownFromArchivedAndLiveTraffic(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	yesterday := today.AddDate(0, 0, -1)
	old := today.AddDate(0, 0, -10)
	f.service.Config.Log.AutoClear = true
	f.service.Config.Log.ClearDays = 7
	add := func(at time.Time, server, download int64) int64 {
		return f.client.TrafficLog.Create().SetServerID(server).SetUserID(14).SetSubscribeID(27).SetTimestamp(at).SetDownload(download).SaveX(ctx).ID
	}
	previous := add(today.Add(-time.Second), 3, 50)
	current := add(today, 3, 80)
	add(today, 4, 900)
	for _, date := range []time.Time{old, yesterday} {
		f.client.SystemLog.Create().SetType(modellog.TypeServerTraffic.Uint8()).SetDate(date.Format(time.DateOnly)).SetObjectID(3).
			SetContent(`{"server_id":3,"download":50,"total":50}`).SaveX(ctx)
	}
	const base = "/v1/admin/log/"
	var servers types.FilterServerTrafficLogResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, base+"server/traffic/list?server_id=3", operator, "", http.StatusOK), &servers))
	require.EqualValues(t, 3, servers.Total)
	require.ElementsMatch(t, []types.ServerTrafficLog{
		{ServerId: 3, Download: 80, Total: 80, Date: today.Format(time.DateOnly), Details: true},
		{ServerId: 3, Download: 50, Total: 50, Date: yesterday.Format(time.DateOnly), Details: true},
		{ServerId: 3, Download: 50, Total: 50, Date: old.Format(time.DateOnly)},
	}, servers.List)
	const details = base + "traffic/details?server_id=3&user_id=14&subscribe_id=27"
	for _, query := range []string{"&date=not-a-date", "&start_date=" + today.Format(time.DateOnly) + "&end_date=" + yesterday.Format(time.DateOnly)} {
		f.request(http.MethodGet, details+query, operator, "", xerr.InvalidParams)
	}
	unavailable := true
	f.client.TrafficLog.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
			if unavailable {
				return nil, errors.New("traffic detail unavailable")
			}
			return next.Query(ctx, q)
		})
	}))
	f.request(http.MethodGet, details, operator, "", xerr.DatabaseQueryError)
	unavailable = false
	for _, selection := range []struct {
		query     string
		id, bytes int64
	}{
		{"", current, 80}, {"&date=" + yesterday.Format(time.DateOnly), previous, 50},
	} {
		var rows types.FilterTrafficLogDetailsResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, details+selection.query, operator, "", http.StatusOK), &rows))
		require.EqualValues(t, 1, rows.Total)
		require.Len(t, rows.List, 1)
		require.Equal(t, selection.id, rows.List[0].Id)
		require.Equal(t, selection.bytes, rows.List[0].Download)
	}
}

func TestPublicStatisticsRoundEnabledUsersAndCacheRegionalTotals(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	var accounts []*ent.UserCreate
	for range 19 {
		accounts = append(accounts, f.client.User.Create().SetPassword("fixture").SetEnable(true))
	}
	accounts = append(accounts, f.client.User.Create().SetPassword("fixture").SetEnable(false), f.client.User.Create().SetPassword("fixture").SetEnable(true).SetDeletedAt(time.Now()))
	f.client.User.CreateBulk(accounts...).ExecX(ctx)
	for _, protocol := range []string{"vless", "vless", "tuic"} {
		f.client.Node.Create().SetProtocol(protocol).SetEnabled(true).SaveX(ctx)
	}
	f.client.Node.Create().SetProtocol("trojan").SetEnabled(false).SaveX(ctx)
	countries := map[string]string{"192.0.2.1": "GB", "192.0.2.2": "GB", "192.0.2.3": "US", "192.0.2.4": ""}
	for addr := range countries {
		f.client.Server.Create().SetAddress(addr).SaveX(ctx)
	}
	var calls int
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = providerTransport(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Host != "ip-api.com" || req.URL.Path != "/batch" {
			return nil, fmt.Errorf("unexpected location request: %s %s", req.Method, req.URL)
		}
		calls++
		var batch []struct{ Query, Fields string }
		if err := json.NewDecoder(req.Body).Decode(&batch); err != nil {
			return nil, err
		}
		var regions []map[string]string
		for _, lookup := range batch {
			country, ok := countries[lookup.Query]
			if !ok || lookup.Fields != "countryCode" {
				return nil, fmt.Errorf("unexpected location lookup: %+v", lookup)
			}
			regions = append(regions, map[string]string{"countryCode": country})
		}
		raw, err := json.Marshal(regions)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
	})
	read := func() types.GetStatResponse {
		t.Helper()
		var stats types.GetStatResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/common/site/stat", "", "", http.StatusOK), &stats))
		return stats
	}
	stats := read()
	require.EqualValues(t, 10, stats.User)
	require.EqualValues(t, 3, stats.Node)
	require.EqualValues(t, 2, stats.Country)
	require.ElementsMatch(t, []string{"vless", "tuic"}, stats.Protocol)
	require.Equal(t, 1, calls)
	f.client.User.Create().SetPassword("fixture").SetEnable(true).SaveX(ctx)
	f.client.Node.Create().SetProtocol("tuic").SetEnabled(true).SaveX(ctx)
	require.Equal(t, stats, read())
	require.Equal(t, 1, calls)
	require.NoError(t, f.service.Redis.Expire(ctx, config.CommonStatCacheKey, 0).Err())
	refreshed := read()
	require.EqualValues(t, 20, refreshed.User)
	require.EqualValues(t, 4, refreshed.Node)
	require.ElementsMatch(t, stats.Protocol, refreshed.Protocol)
	require.Equal(t, 2, calls)
}
