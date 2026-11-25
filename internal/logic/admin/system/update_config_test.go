package system

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/logic/common"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/requestmeta"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

var configFixtureID atomic.Uint64

func settingsFixture(t *testing.T) (*svc.ServiceContext, *ent.Client) {
	t.Helper()
	database, err := sql.Open("sqlite", fmt.Sprintf("file:settings-%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", configFixtureID.Add(1)))
	if err != nil {
		t.Fatal(err)
	}
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	return &svc.ServiceContext{Store: repository.NewStore(client, nil)}, client
}

func TestSettingsAuditOnlyChangedKeys(t *testing.T) {
	service, client := settingsFixture(t)
	ctx := requestmeta.WithActor(context.Background(), 7)
	fields := []configFieldValue{{key: "Secret", value: "private-value"}, {key: "Name", value: "panel"}}
	for attempt := 0; attempt < 2; attempt++ {
		if err := updateConfigFields(ctx, service, "test", fields); err != nil {
			t.Fatal(err)
		}
	}
	logs := client.SystemLog.Query().AllX(ctx)
	if len(logs) != 1 || logs[0].ObjectID != 7 || logs[0].Type != 50 || !strings.Contains(logs[0].Content, "keys=Name,Secret") || strings.Contains(logs[0].Content, "private-value") {
		t.Fatalf("settings audit leaked values or omitted changes: %+v", logs)
	}
}

func TestSettingsAuditFailureRollsBackValues(t *testing.T) {
	service, client := settingsFixture(t)
	ctx := context.Background()
	if err := client.Driver().Exec(ctx, "DROP TABLE system_logs", []any{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := updateConfigFields(ctx, service, "test", []configFieldValue{{key: "Secret", value: "private-value"}}); err == nil {
		t.Fatal("audit failure ignored")
	}
	if client.System.Query().CountX(ctx) != 0 {
		t.Fatal("settings committed without their audit")
	}
}

func TestSiteAndLegalUpdatesReachPublicSettingsAndInvalidateCache(t *testing.T) {
	service, db := settingsFixture(t)
	ctx := requestmeta.WithActor(t.Context(), 7)
	cache := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	service.Redis = redisClient
	for _, key := range []string{config.SiteConfigKey, config.GlobalConfigKey, config.TosConfigKey} {
		if err := cache.Set(key, "previous settings"); err != nil {
			t.Fatal(err)
		}
	}
	db.System.Create().SetCategory("ad").SetKey("WebAD").SetValue("false").SetType("bool").SetDesc("fixture").SaveX(ctx)
	want := &types.SiteConfig{Host: "https://panel.example.test", SiteName: "Example Panel", SiteDesc: "Account portal"}
	if err := NewUpdateSiteConfigLogic(ctx, service).UpdateSiteConfig(want); err != nil {
		t.Fatal(err)
	}
	if got, err := NewGetSiteConfigLogic(ctx, service).GetSiteConfig(); err != nil || *got != *want {
		t.Fatalf("admin site config = %+v, %v", got, err)
	}
	if cache.Exists(config.SiteConfigKey) || cache.Exists(config.GlobalConfigKey) {
		t.Fatal("site update retained stale settings in Redis")
	}
	if got, err := common.NewGetGlobalConfigLogic(ctx, service).GetGlobalConfig(); err != nil || got.Site != *want {
		t.Fatalf("public site config = %+v, %v", got, err)
	}
	if err := NewUpdateTosConfigLogic(ctx, service).UpdateTosConfig(&types.TosConfig{TosContent: "New terms"}); err != nil {
		t.Fatal(err)
	}
	if err := NewUpdatePrivacyPolicyConfigLogic(ctx, service).UpdatePrivacyPolicyConfig(&types.PrivacyPolicyConfig{PrivacyPolicy: "New policy"}); err != nil {
		t.Fatal(err)
	}
	if got, err := common.NewGetTosLogic(ctx, service).GetTos(); err != nil || got.TosContent != "New terms" {
		t.Fatalf("public terms = %+v, %v", got, err)
	}
	if got, err := common.NewGetPrivacyPolicyLogic(ctx, service).GetPrivacyPolicy(); err != nil || got.PrivacyPolicy != "New policy" {
		t.Fatalf("public privacy policy = %+v, %v", got, err)
	}
	if cache.Exists(config.TosConfigKey) {
		t.Fatal("legal updates retained stale settings in Redis")
	}
}

func TestRegistrationSettingsRefreshRuntimeAndPreserveNumericValues(t *testing.T) {
	service, _ := settingsFixture(t)
	ctx := t.Context()
	wantRegister := &types.RegisterConfig{StopRegister: true, EnableTrial: true, TrialSubscribe: 42, TrialTime: 15,
		TrialTimeUnit: "day", EnableIpRegisterLimit: true, IpRegisterLimit: 3, IpRegisterLimitDuration: 30, DeviceLimit: 2}
	if err := NewUpdateRegisterConfigLogic(ctx, service).UpdateRegisterConfig(wantRegister); err != nil {
		t.Fatal(err)
	}
	if got, err := NewGetRegisterConfigLogic(ctx, service).GetRegisterConfig(); err != nil || *got != *wantRegister {
		t.Fatalf("stored registration config = %+v, %v", got, err)
	}
	if got := service.Config.Register; !got.StopRegister || !got.EnableTrial || got.TrialSubscribe != 42 || !got.EnableIpRegisterLimit || got.IpRegisterLimit != 3 {
		t.Fatalf("live registration config = %+v", got)
	}
	wantCode := &types.VerifyCodeConfig{VerifyCodeExpireTime: 300, VerifyCodeLimit: 11, VerifyCodeInterval: 75}
	if err := NewUpdateVerifyCodeConfigLogic(ctx, service).UpdateVerifyCodeConfig(wantCode); err != nil {
		t.Fatal(err)
	}
	if got, err := NewGetVerifyCodeConfigLogic(ctx, service).GetVerifyCodeConfig(); err != nil || *got != *wantCode {
		t.Fatalf("verification code policy = %+v, %v", got, err)
	}
}

func TestMaskedSettingsRoundTrip(t *testing.T) {
	service, _ := settingsFixture(t)
	ctx := context.Background()
	for _, setting := range []struct{ category, key, value string }{
		{"verify", "TurnstileSecret", "turnstile-secret"},
		{"currency", "AccessKey", "currency-secret"},
	} {
		if err := updateConfigFields(ctx, service, setting.category, []configFieldValue{{key: setting.key, value: secretMask}}); err == nil {
			t.Fatal("unknown masked secret accepted")
		}
		if err := updateConfigFields(ctx, service, setting.category, []configFieldValue{{key: setting.key, value: setting.value}}); err != nil {
			t.Fatal(err)
		}
		if err := updateConfigFields(ctx, service, setting.category, []configFieldValue{{key: setting.key, value: secretMask}}); err != nil {
			t.Fatal(err)
		}
		stored, err := service.Store.System().FindByCategoryKey(ctx, setting.category, setting.key)
		if err != nil || stored.Value != setting.value {
			t.Fatalf("masked secret lost: %+v, %v", stored, err)
		}
	}
	currency, err := NewGetCurrencyConfigLogic(ctx, service).GetCurrencyConfig()
	if err != nil || currency.AccessKey != secretMask {
		t.Fatalf("currency secret exposed: %+v, %v", currency, err)
	}
	previous := `[ {"name":"first","password":"one","uuid":"uuid-one"}, {"name":"second","password":"two"} ]`
	requested := `[{"name":"second","password":"********"},{"name":"renamed","password":"********"}]`
	value, err := keepSettingsSecrets("server", "Outbound", requested, previous)
	if err != nil {
		t.Fatal(err)
	}
	var outbounds []types.NodeOutbound
	if err := json.Unmarshal([]byte(value), &outbounds); err != nil {
		t.Fatal(err)
	}
	if outbounds[0].Password != "two" || outbounds[1].Password != "two" {
		t.Fatalf("outbound matching = %+v", outbounds)
	}
	if _, err := keepSettingsSecrets("server", "Outbound", `[{"name":"first","uuid":"********"}]`, `[{"name":"first"}]`); err == nil {
		t.Fatal("unknown outbound credential accepted")
	}
	if err := updateConfigFields(ctx, service, "server", []configFieldValue{{key: "Outbound", value: previous}, {key: "NodeSecret", value: "deployment-secret"}}); err != nil {
		t.Fatal(err)
	}
	view, err := NewGetNodeConfigLogic(ctx, service).GetNodeConfig()
	if err != nil || view.NodeSecret != "deployment-secret" || view.Outbound[0].Password != secretMask || view.Outbound[0].UUID != secretMask || view.Outbound[1].UUID != "" {
		t.Fatalf("node config secret exposure: %+v, %v", view, err)
	}
}
