package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/types"
	"github.com/stretchr/testify/require"
)

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
