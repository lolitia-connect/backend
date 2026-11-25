package initialize

import (
	"database/sql"
	"slices"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tool"
	_ "modernc.org/sqlite"
)

func TestNodeBootstrapLoadsPolicyAndPreservesProvisionedSecrets(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil)}
	secret := client.System.Create().SetCategory("server").SetKey("NodeSecret").SetType("string").SetDesc("Node secret").SetValue("").SaveX(ctx)
	for _, setting := range []struct{ key, value, kind string }{
		{"NodePullInterval", "25", "int64"},
		{"NodePushInterval", "40", "int64"},
		{"DNS", `[{"proto":"tls","address":"resolver.example.test","domains":["example.test"]}]`, "string"},
		{"Block", `["private","sensitive","private"]`, "string"},
		{"Outbound", `[{"name":"relay","protocol":"socks","address":"relay.example.test","port":1080}]`, "string"},
		{"NodeMultiplierConfig", `[{"start_time":"22:00","end_time":"06:00","multiplier":1.5}]`, "string"},
	} {
		client.System.Create().SetCategory("server").SetKey(setting.key).SetValue(setting.value).SetType(setting.kind).SetDesc(setting.key).SaveX(ctx)
	}
	NodeSecret(service)
	generated := client.System.GetX(ctx, secret.ID).Value
	if len(generated) < 32 || generated == tool.LegacyDefaultNodeSecret {
		t.Fatal("fresh install did not persist a new node secret")
	}
	NodeSecret(service)
	Node(service)
	policy := service.Config.Node
	if policy.NodeSecret != generated || client.System.GetX(ctx, secret.ID).Value != generated || policy.NodePullInterval != 25 || policy.NodePushInterval != 40 {
		t.Fatal("node bootstrap changed the provisioned secret or failed to load custom intervals")
	}
	if len(policy.DNS) != 1 || policy.DNS[0].Address != "resolver.example.test" || !slices.Equal(policy.DNS[0].Domains, []string{"example.test"}) ||
		!slices.Equal(policy.Block, []string{"private", "sensitive"}) || len(policy.Outbound) != 1 || policy.Outbound[0].Address != "relay.example.test" || policy.Outbound[0].Port != 1080 {
		t.Fatal("node bootstrap failed to load DNS, blocklist, and outbound policy")
	}
	manager := service.NodeMultiplierManager
	if manager == nil || manager.GetMultiplier(time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)) != 1.5 ||
		manager.GetMultiplier(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)) != 1 {
		t.Fatal("persisted multiplier did not apply across midnight")
	}
	client.System.UpdateOneID(secret.ID).SetValue(tool.LegacyDefaultNodeSecret).SaveX(ctx)
	NodeSecret(service)
	if got := client.System.GetX(ctx, secret.ID).Value; got != tool.LegacyDefaultNodeSecret {
		t.Fatal("bootstrap rotated a deployed legacy secret")
	}
}
