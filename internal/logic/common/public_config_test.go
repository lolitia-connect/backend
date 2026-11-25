package common

import (
	"database/sql"
	"slices"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	_ "modernc.org/sqlite"
)

func newPublicConfigDB(t *testing.T) *ent.Client {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
}

func TestPublicConfigurationUsesPersistedSettingsAndEnabledAuthMethods(t *testing.T) {
	ctx := t.Context()
	client := newPublicConfigDB(t)
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil)}
	service.Config.Site.SiteName = "Example portal"
	service.Config.Subscribe.SubscribePath = "/links"
	for _, setting := range []struct{ category, key, value, kind string }{
		{"currency", "CurrencyUnit", "EUR", "string"},
		{"currency", "CurrencySymbol", "€", "string"},
		{"verify_code", "VerifyCodeInterval", "120", "int"},
		{"ad", "WebAD", "true", "bool"},
		{"tos", "TosContent", "Example terms", "string"},
		{"tos", "PrivacyPolicy", "Example policy", "string"},
	} {
		client.System.Create().SetCategory(setting.category).SetKey(setting.key).SetValue(setting.value).
			SetType(setting.kind).SetDesc("fixture").SaveX(ctx)
	}
	client.AuthMethod.Create().SetMethod("device").SetEnabled(true).SetConfig(`{}`).SaveX(ctx)
	client.AuthMethod.Create().SetMethod("github").SetEnabled(true).SetConfig(`{}`).SaveX(ctx)
	client.AuthMethod.Create().SetMethod("google").SetEnabled(false).SetConfig(`{}`).SaveX(ctx)
	t.Setenv("GATEWAY_PORT", "8888")
	cfg := NewGetGlobalConfigLogic(ctx, service)
	for _, tc := range []struct{ gateway, wantPath string }{{"false", "/links"}, {"true", "/sub/links"}} {
		t.Setenv("GATEWAY_MODE", tc.gateway)
		got, err := cfg.GetGlobalConfig()
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(got.OAuthMethods)
		if got.Site.SiteName != "Example portal" || got.Subscribe.SubscribePath != tc.wantPath || !got.WebAd ||
			got.Currency.CurrencyUnit != "EUR" || got.Currency.CurrencySymbol != "€" || got.VerifyCode.VerifyCodeInterval != 120 ||
			!got.Auth.Device.Enable || !slices.Equal(got.OAuthMethods, []string{"device", "github"}) {
			t.Fatalf("gateway mode %s config = %+v", tc.gateway, got)
		}
	}
	tos, err := NewGetTosLogic(ctx, service).GetTos()
	if err != nil || tos.TosContent != "Example terms" {
		t.Fatalf("terms = %+v, %v", tos, err)
	}
	policy, err := NewGetPrivacyPolicyLogic(ctx, service).GetPrivacyPolicy()
	if err != nil || policy.PrivacyPolicy != "Example policy" {
		t.Fatalf("policy = %+v, %v", policy, err)
	}
}
