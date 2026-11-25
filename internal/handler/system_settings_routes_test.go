package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestOperatorReadsLatestLogWindowAfterFileBecomesAvailable(t *testing.T) {
	f := newRouteFixture(t)
	_, operator := f.account(true)
	_, member := f.account(false)
	dir := t.TempDir()
	f.service.Config.Logger.Path = dir
	const path = "/v1/admin/tool/log"
	require.Empty(t, f.request(http.MethodGet, path, operator, "", xerr.ERROR))
	var records strings.Builder
	for i := 1; i <= 55; i++ {
		if i == 40 {
			records.WriteString("incomplete log entry\r\n")
			continue
		}
		fmt.Fprintf(&records, "{\"content\":\"request %d\",\"sequence\":%d}\r\n", i, i)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "access.log"), []byte(records.String()), 0600))
	require.Empty(t, f.request(http.MethodGet, path, member, "", xerr.InvalidAccess))
	var result struct {
		List []struct {
			Content  string `json:"content"`
			Sequence int    `json:"sequence"`
		} `json:"list"`
	}
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path, operator, "", http.StatusOK), &result))
	require.Len(t, result.List, 49)
	next := 6
	for _, record := range result.List {
		if next == 40 {
			next++
		}
		require.Equal(t, next, record.Sequence)
		require.Equal(t, fmt.Sprintf("request %d", next), record.Content)
		next++
	}
}

func TestSettingsEditorRetainsPublishedValuesUntilAuditCommits(t *testing.T) {
	f := newRouteFixture(t)
	_, operator := f.account(true)
	var offline, auditUnavailable bool
	f.client.System.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
			if offline {
				return nil, errors.New("settings database unavailable")
			}
			return next.Query(ctx, q)
		})
	}))
	f.client.SystemLog.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if auditUnavailable {
				return nil, errors.New("audit storage is read-only")
			}
			return next.Mutate(ctx, m)
		})
	})
	for _, tc := range []struct {
		editor, field         string
		original, replacement any
	}{
		{"subscribe_config", "subscribe_domain", "downloads.example.test", "plans.example.test"},
		{"site_config", "site_name", "Member portal", "Customer portal"},
		{"privacy", "privacy_policy", "Privacy policy v1", "Privacy policy v2"},
		{"tos_config", "tos_content", "Terms v1", "Terms v2"},
		{"invite_config", "referral_percentage", 3, 7},
		{"register_config", "device_limit", 2, 5},
		{"verify_config", "turnstile_site_key", "site-key-v1", "site-key-v2"},
		{"verify_code_config", "verify_code_interval", 60, 90},
		{"currency_config", "currency_unit", "EUR", "USD"},
		{"node_config", "node_pull_interval", 15, 45},
	} {
		t.Run(tc.editor, func(t *testing.T) {
			path := "/v1/admin/system/" + tc.editor
			put := func(value any, want uint32) {
				t.Helper()
				form := map[string]any{tc.field: value}
				if tc.editor == "node_config" {
					form["outbound"] = []types.NodeOutbound{}
				}
				body, err := json.Marshal(form)
				require.NoError(t, err)
				f.request(http.MethodPut, path, operator, string(body), want)
			}
			check := func(want any) {
				t.Helper()
				var settings map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path, operator, "", http.StatusOK), &settings))
				expected, err := json.Marshal(want)
				require.NoError(t, err)
				require.JSONEq(t, string(expected), string(settings[tc.field]))
			}
			put(tc.original, http.StatusOK)
			check(tc.original)
			audits := f.client.SystemLog.Query().CountX(t.Context())
			put([]string{"invalid form value"}, xerr.InvalidParams)
			auditUnavailable = true
			put(tc.replacement, xerr.DatabaseUpdateError)
			auditUnavailable = false
			require.Equal(t, audits, f.client.SystemLog.Query().CountX(t.Context()))
			check(tc.original)
			offline = true
			require.Empty(t, f.request(http.MethodGet, path, operator, "", xerr.DatabaseQueryError))
			offline = false
			put(tc.replacement, http.StatusOK)
			check(tc.replacement)
			require.Equal(t, audits+1, f.client.SystemLog.Query().CountX(t.Context()))
		})
	}
}

func TestPublishedCheckoutAndRegistrationSettingsHideProviderCredentials(t *testing.T) {
	f := newRouteFixture(t)
	t.Setenv("GATEWAY_MODE", "false")
	_, admin := f.account(true)
	_, customer := f.account(false)
	f.client.System.Create().SetCategory("ad").SetKey("WebAD").SetValue("false").SetType("bool").SetDesc("fixture").SaveX(t.Context())
	const base = "/v1/admin/system/"
	const currencyPath = base + "currency_config"
	const verifyPath = base + "verify_config"
	const key = "fixture-exchange-credential"
	const secret = "fixture-verification-credential"
	put := func(path string, value any) {
		t.Helper()
		body, err := json.Marshal(value)
		require.NoError(t, err)
		f.request(http.MethodPut, path, admin, string(body), http.StatusOK)
	}
	public := func() types.GetGlobalConfigResponse {
		t.Helper()
		raw := f.request(http.MethodGet, "/v1/common/site/config", "", "", http.StatusOK)
		require.NotContains(t, string(raw), key)
		require.NotContains(t, string(raw), secret)
		var got types.GetGlobalConfigResponse
		require.NoError(t, json.Unmarshal(raw, &got))
		return got
	}
	before := public()
	f.request(http.MethodPut, currencyPath, customer, `{"currency_unit":"AUD"}`, xerr.InvalidAccess)
	require.Equal(t, before.Currency, public().Currency)
	put(currencyPath, types.CurrencyConfig{CurrencyUnit: "EUR", CurrencySymbol: "€", AccessKey: key})
	put(verifyPath, types.VerifyConfig{CaptchaType: "turnstile", TurnstileSiteKey: "fixture-site", TurnstileSecret: secret, EnableUserRegisterCaptcha: true})
	invite := types.InviteConfig{ForcedInvite: true, ReferralPercentage: 7, OnlyFirstPurchase: true}
	put(base+"invite_config", invite)
	settings := public()
	require.Equal(t, types.Currency{CurrencyUnit: "EUR", CurrencySymbol: "€"}, settings.Currency)
	require.Equal(t, invite, settings.Invite)
	require.True(t, settings.Verify.EnableUserRegisterCaptcha)
	require.Equal(t, "fixture-site", settings.Verify.TurnstileSiteKey)

	var currency types.CurrencyConfig
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, currencyPath, admin, "", http.StatusOK), &currency))
	require.Equal(t, "********", currency.AccessKey)
	currency.CurrencyUnit, currency.CurrencySymbol = "JPY", "¥"
	put(currencyPath, currency)
	var verify types.VerifyConfig
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, verifyPath, admin, "", http.StatusOK), &verify))
	require.Equal(t, "********", verify.TurnstileSecret)
	verify.EnableUserLoginCaptcha = true
	put(verifyPath, verify)
	settings = public()
	require.Equal(t, types.Currency{CurrencyUnit: "JPY", CurrencySymbol: "¥"}, settings.Currency)
	require.True(t, settings.Verify.EnableUserLoginCaptcha)
	require.Equal(t, key, f.service.Config.Currency.AccessKey)
	require.Equal(t, secret, f.service.Config.Verify.TurnstileSecret)
}

func TestNodeSettingsKeepOutboundCredentialsWhenOperatorReordersRoutes(t *testing.T) {
	f := newRouteFixture(t)
	_, admin := f.account(true)
	const path = "/v1/admin/system/node_config"
	const first = "fixture-east-password"
	const second = "fixture-west-uuid"
	put := func(config types.NodeConfig) {
		t.Helper()
		raw, err := json.Marshal(config)
		require.NoError(t, err)
		f.request(http.MethodPut, path, admin, string(raw), http.StatusOK)
	}
	cfg := types.NodeConfig{NodePullInterval: 25, DNS: []types.NodeDNS{{Proto: "udp", Address: "192.0.2.53", Domains: []string{"example.test"}}}, Block: []string{"blocked.example.test", "blocked.example.test"}, Outbound: []types.NodeOutbound{{Name: "east", Password: first}, {Name: "west", UUID: second}}}
	put(cfg)
	raw := f.request(http.MethodGet, path, admin, "", http.StatusOK)
	require.NotContains(t, string(raw), first)
	require.NotContains(t, string(raw), second)
	var read types.NodeConfig
	require.NoError(t, json.Unmarshal(raw, &read))
	require.Equal(t, cfg.DNS, read.DNS)
	require.Equal(t, []string{"blocked.example.test"}, read.Block)
	require.Len(t, read.Outbound, 2)
	require.Equal(t, "********", read.Outbound[0].Password)
	require.Equal(t, "********", read.Outbound[1].UUID)
	read.Outbound[0], read.Outbound[1] = read.Outbound[1], read.Outbound[0]
	read.NodePullInterval = 30
	put(read)
	applied := f.service.Config.Node
	require.EqualValues(t, 30, applied.NodePullInterval)
	require.Len(t, applied.Outbound, 2)
	require.Equal(t, "west", applied.Outbound[0].Name)
	require.Equal(t, second, applied.Outbound[0].UUID)
	require.Empty(t, applied.Outbound[0].Password)
	require.Equal(t, first, applied.Outbound[1].Password)
}

func TestServerOverridesCanReturnIndividualFieldsToGlobalSettings(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, admin := f.account(true)
	primary := f.client.Server.Create().SetName("Primary").SaveX(ctx)
	backup := f.client.Server.Create().SetName("Backup").SaveX(ctx)
	f.service.Config.Node.IPStrategy = "prefer_ipv4"
	f.service.Config.Node.Block = []string{"global.example.test"}
	const base = "/v1/admin/server/node_config"
	read := func(id int64) types.GetServerNodeConfigResponse {
		t.Helper()
		var result types.GetServerNodeConfigResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, fmt.Sprintf("%s?server_id=%d", base, id), admin, "", http.StatusOK), &result))
		return result
	}
	save := func(override types.ServerNodeConfigOverride) {
		t.Helper()
		body, err := json.Marshal(types.UpdateServerNodeConfigRequest{ServerID: primary.ID, ServerNodeConfigOverride: override})
		require.NoError(t, err)
		f.request(http.MethodPost, base+"/update", admin, string(body), http.StatusOK)
	}
	defaults := read(backup.ID)
	require.Equal(t, defaults.Global, defaults.Effective)
	custom := types.ServerNodeConfigOverride{IPStrategy: "prefer_ipv6", DNS: []types.NodeDNS{{Proto: "udp", Address: "192.0.2.53"}}, Block: []string{"local.example.test"}, Outbound: []types.NodeOutbound{{Name: "local", Protocol: "direct"}}}
	save(custom)
	applied := read(primary.ID)
	require.Equal(t, custom.IPStrategy, applied.Effective.IPStrategy)
	require.Equal(t, custom.DNS[0].Address, applied.Effective.DNS[0].Address)
	require.Equal(t, custom.Block, applied.Effective.Block)
	require.Equal(t, custom.Outbound[0].Name, applied.Effective.Outbound[0].Name)
	custom.InheritIPStrategy, custom.InheritDNS, custom.InheritOutbound = true, true, true
	custom.Block = []string{}
	save(custom)
	partial := read(primary.ID)
	require.True(t, partial.Override.InheritIPStrategy)
	require.True(t, partial.Override.InheritDNS)
	require.True(t, partial.Override.InheritOutbound)
	require.False(t, partial.Override.InheritBlock)
	require.Equal(t, defaults.Global.IPStrategy, partial.Effective.IPStrategy)
	require.Equal(t, defaults.Global.DNS, partial.Effective.DNS)
	require.Equal(t, defaults.Global.Outbound, partial.Effective.Outbound)
	require.Empty(t, partial.Effective.Block)
	require.Equal(t, defaults, read(backup.ID))
	custom.InheritBlock = true
	save(custom)
	require.Equal(t, defaults, read(primary.ID))
	require.Zero(t, f.client.ServerConfigOverride.Query().CountX(ctx))
}

func TestTrafficMultiplierPreviewUsesSavedSchedule(t *testing.T) {
	f := newRouteFixture(t)
	_, admin := f.account(true)
	const base = "/v1/admin/system/"
	set := func(periods []types.TimePeriod) {
		t.Helper()
		body, err := json.Marshal(types.SetNodeMultiplierRequest{Periods: periods})
		require.NoError(t, err)
		f.request(http.MethodPost, base+"set_node_multiplier", admin, string(body), http.StatusOK)
	}
	preview := func() float32 {
		t.Helper()
		var current types.PreViewNodeMultiplierResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, base+"node_multiplier/preview", admin, "", http.StatusOK), &current))
		return current.Ratio
	}
	periods := []types.TimePeriod{{StartTime: "00:00", EndTime: "23:59", Multiplier: 1.5}}
	set(periods)
	var saved types.GetNodeMultiplierResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, base+"get_node_multiplier", admin, "", http.StatusOK), &saved))
	require.Equal(t, periods, saved.Periods)
	require.EqualValues(t, 1.5, preview())
	set([]types.TimePeriod{})
	require.EqualValues(t, 1, preview())
}
