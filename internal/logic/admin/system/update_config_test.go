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
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/requestmeta"
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
