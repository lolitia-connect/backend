package nekoproxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	remote "github.com/perfect-panel/server/pkg/nekoproxy"
	_ "modernc.org/sqlite"
)

var fixtureID atomic.Uint64

type fixture struct {
	logic        *Logic
	client       *ent.Client
	url          string
	port         int
	targetPort   int
	patchStatus  int
	patches      int
	issueFailure bool
	unavailable  bool
	deletes      int
}

func setupIntegration(t *testing.T) *fixture {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:nekoproxy-%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", fixtureID.Add(1)))
	if err != nil {
		t.Fatal(err)
	}
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	client.Node.Create().SetID(1).SetName("existing").SetAddress("old.example.com").SetPort(443).SetServerID(99).SetProtocol("vless").SetProtocolID("2").SetSort(1).SaveX(ctx)
	client.Node.Create().SetID(2).SetName("other").SetSort(2).SaveX(ctx)
	f := &fixture{client: client, port: 2345, targetPort: 443}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer nkp_test" || !strings.HasPrefix(r.URL.Path, "/api/v1/l4/") {
			t.Error("incorrect token API request")
		}
		if f.unavailable {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":"API_TOKEN_REQUIRED"}`)
			return
		}
		service := remote.Service{ID: "l4s-1", Enabled: true, TargetPort: f.targetPort}
		session := remote.Session{ServiceID: "l4s-1", SessionID: "session-1", NodeHost: "entry.example.com", AllocatedPort: f.port, Status: "active"}
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/l4/services/l4s-1/target-port":
			f.patches++
			var body map[string]int
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 2 || body["expectedTargetPort"] != f.targetPort || body["targetPort"] <= 0 {
				t.Errorf("bad guarded port update: %v / %v", body, err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if f.patchStatus != 0 {
				w.WriteHeader(f.patchStatus)
				return
			}
			f.targetPort = body["targetPort"]
			fmt.Fprint(w, `{"ok":true}`)
		case r.Method == http.MethodDelete:
			f.deletes++
			fmt.Fprint(w, `{"ok":true}`)
		case strings.HasSuffix(r.URL.Path, "/services/l4s-1/sessions"):
			if f.issueFailure {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"error":"PORT_IN_USE"}`)
				return
			}
			_ = json.NewEncoder(w).Encode(session)
		case strings.HasSuffix(r.URL.Path, "/sessions"):
			_ = json.NewEncoder(w).Encode(map[string]any{"rows": []remote.Session{session}})
		case strings.HasSuffix(r.URL.Path, "/services/l4s-1") || r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(service)
		case strings.HasSuffix(r.URL.Path, "/services"):
			_ = json.NewEncoder(w).Encode([]remote.Service{service})
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	t.Cleanup(server.Close)
	f.url = server.URL
	store := repository.NewStore(client, nil)
	payload, _ := json.Marshal(connection{APIURL: server.URL, Token: "nkp_test"})
	if err := store.System().UpsertByCategoryKey(ctx, "nekoproxy", configKey, string(payload), "string", ""); err != nil {
		t.Fatal(err)
	}
	f.logic = NewLogic(ctx, &svc.ServiceContext{Store: store, Ent: client})
	return f
}

func TestNekoproxyBindingSyncConstraintsAndCascade(t *testing.T) {
	f := setupIntegration(t)
	ctx := context.Background()
	if err := f.logic.Bind(&types.BindNekoproxyRequest{NodeID: 1, ForwardID: "l4s-1", SessionID: "session-1"}); err != nil {
		t.Fatal(err)
	}
	n := f.client.Node.GetX(ctx, 1)
	if n.Address != "entry.example.com" || n.Port != 2345 || n.ServerID != 99 || n.Protocol != "vless" || n.ProtocolID != "2" {
		t.Fatalf("sync changed unrelated node fields: %+v", n)
	}
	if err := f.logic.Bind(&types.BindNekoproxyRequest{NodeID: 2, ForwardID: "l4s-1", SessionID: "session-1"}); err == nil {
		t.Fatal("duplicate forward binding accepted")
	}
	if f.client.Node.GetX(ctx, 2).Address != "" {
		t.Fatal("failed binding partially updated node")
	}
	if err := f.client.NekoproxyBinding.Create().SetNodeID(999).SetAPIURL(f.url).SetForwardID("missing").SetSessionID("missing").Exec(ctx); err == nil {
		t.Fatal("foreign key did not reject nonexistent node")
	}
	f.port = 3456
	result, err := f.logic.Sync(&types.SyncNekoproxyRequest{})
	if err != nil || result.Updated != 1 || len(result.Errors) != 0 || f.client.Node.GetX(ctx, 1).Port != 3456 {
		t.Fatalf("sync failed: %+v %v", result, err)
	}
	f.port = 0
	result, err = f.logic.Sync(&types.SyncNekoproxyRequest{})
	if err != nil || result.Updated != 0 || len(result.Errors) != 1 || f.client.Node.GetX(ctx, 1).Port != 3456 {
		t.Fatalf("invalid remote endpoint overwrote node: %+v %v", result, err)
	}
	f.unavailable = true
	state, stateErr := f.logic.State()
	if stateErr != nil || state.Error == "" || len(state.Bindings) != 1 {
		t.Fatalf("remote outage hid local bindings: %+v %v", state, stateErr)
	}
	if err := f.client.Node.DeleteOneID(1).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if f.client.NekoproxyBinding.Query().CountX(ctx) != 0 || f.deletes != 0 {
		t.Fatal("local deletion must only cascade the local binding")
	}
}

func newForwardOptions() *types.NekoproxyNodeOptions {
	return &types.NekoproxyNodeOptions{Create: &types.CreateNekoproxyForward{ChainID: "chain-1", EntryGroupID: "group-1", TargetHost: "target.example.com", TargetPort: 443, Protocol: "tcp_udp"}}
}

func TestNekoproxyCreateRollsBackRemoteAndLocalFailures(t *testing.T) {
	f := setupIntegration(t)
	ctx := context.Background()
	f.issueFailure = true
	if err := f.logic.CreateNode(&node.Node{Name: "new"}, newForwardOptions()); err == nil {
		t.Fatal("failed allocation accepted")
	}
	if f.deletes != 1 || f.client.Node.Query().CountX(ctx) != 2 {
		t.Fatal("failed allocation left remote service or local node")
	}
	f.issueFailure = false
	if err := f.logic.Bind(&types.BindNekoproxyRequest{NodeID: 1, ForwardID: "l4s-1", SessionID: "session-1"}); err != nil {
		t.Fatal(err)
	}
	if err := f.logic.CreateNode(&node.Node{Name: "new"}, newForwardOptions()); err == nil {
		t.Fatal("binding constraint ignored")
	}
	if f.deletes != 2 || f.client.Node.Query().CountX(ctx) != 2 {
		t.Fatal("failed binding did not roll back node and created service")
	}
	if err := f.logic.Unbind(&types.UnbindNekoproxyRequest{NodeID: 1}); err != nil {
		t.Fatal(err)
	}
	data := &node.Node{Name: "new", Protocol: "vless"}
	if err := f.logic.CreateNode(data, newForwardOptions()); err != nil {
		t.Fatal(err)
	}
	if data.Id == 0 || f.client.Node.GetX(ctx, data.Id).Port != 2345 || f.client.NekoproxyBinding.Query().CountX(ctx) != 1 {
		t.Fatal("successful create did not persist node and binding")
	}
}

func TestNekoproxyNodeSavePreferenceAndAtomicSync(t *testing.T) {
	f := setupIntegration(t)
	ctx := t.Context()
	if err := f.logic.Bind(&types.BindNekoproxyRequest{NodeID: 1, ForwardID: "l4s-1", SessionID: "session-1"}); err != nil {
		t.Fatal(err)
	}
	data, err := f.logic.svc.Store.Node().FindOneNode(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	data.Name, data.Address = "edited", "drift.example.test"
	f.port = 3456
	if err := f.logic.UpdateNode(data, nil); err != nil {
		t.Fatal(err)
	}
	entry, err := f.logic.NodeEntry(1)
	if err != nil || entry.Binding == nil || !entry.Binding.SyncOnSave || entry.Port != 3456 || entry.Address != "entry.example.com" {
		t.Fatalf("default save: %+v %v", entry, err)
	}
	f.unavailable = true
	off, on := false, true
	data.Address, data.Port = "manual.example.test", 443
	if err := f.logic.UpdateNode(data, &off); err != nil {
		t.Fatal(err)
	}
	data.Name = "saved offline"
	if err := f.logic.UpdateNode(data, nil); err != nil {
		t.Fatal(err)
	}
	data.Name = "must not persist"
	if err := f.logic.UpdateNode(data, &on); err == nil {
		t.Fatal("accepted failed remote sync")
	}
	entry, err = f.logic.NodeEntry(1)
	if err != nil || entry.Binding.SyncOnSave || entry.Address != "manual.example.test" || entry.Port != 443 || f.client.Node.GetX(ctx, 1).Name != "saved offline" {
		t.Fatalf("failed save changed node or preference: %+v %v", entry, err)
	}
	f.unavailable = false
	result, err := f.logic.Sync(&types.SyncNekoproxyRequest{NodeID: 1})
	if err != nil || result.Updated != 1 || f.client.Node.GetX(ctx, 2).Port != 0 {
		t.Fatalf("manual node sync: %+v %v", result, err)
	}
	entry, err = f.logic.NodeEntry(1)
	if err != nil || entry.Binding.SyncOnSave || entry.Port != 3456 {
		t.Fatalf("manual sync changed preference: %+v %v", entry, err)
	}
	if err := f.logic.UpdateNode(data, &on); err != nil {
		t.Fatal(err)
	}
	entry, err = f.logic.NodeEntry(1)
	if err != nil || !entry.Binding.SyncOnSave || entry.Port != 3456 {
		t.Fatalf("re-enabled save: %+v %v", entry, err)
	}
}

func (f *fixture) seedProtocols(t *testing.T) {
	t.Helper()
	f.client.Server.Create().SetID(99).SetProtocols(`[{"type":"trojan","id":"2","port":9443,"enable":true},{"type":"vless","id":"2","port":443,"enable":true},{"type":"vless","id":"3","port":8443,"enable":true}]`).SaveX(t.Context())
}

func TestNekoproxyProtocolSwitchUsesInstancePortNotEntryPort(t *testing.T) {
	f := setupIntegration(t)
	f.seedProtocols(t)
	ctx := t.Context()
	if err := f.logic.Bind(&types.BindNekoproxyRequest{NodeID: 1, ForwardID: "l4s-1", SessionID: "session-1"}); err != nil {
		t.Fatal(err)
	}
	edit, err := f.logic.svc.Store.Node().FindOneNode(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	edit.ProtocolId, edit.Name = "3", "changed"
	f.patchStatus = http.StatusConflict
	if err := f.logic.UpdateNode(edit, nil); err == nil {
		t.Fatal("failed target update accepted")
	}
	if n := f.client.Node.GetX(ctx, 1); n.ProtocolID != "2" || n.Name == "changed" || f.targetPort != 443 {
		t.Fatal("failed patch partially saved the edit")
	}
	f.patchStatus = 0
	if err := f.logic.UpdateNode(edit, nil); err != nil {
		t.Fatal(err)
	}
	if n := f.client.Node.GetX(ctx, 1); n.ProtocolID != "3" || n.Port != uint16(f.port) || f.targetPort != 8443 {
		t.Fatalf("wrong entry or target: %d/%d", n.Port, f.targetPort)
	}
	// An operator can pin a different target directly in Nekoproxy, even if it matches a later selection.
	f.targetPort, edit.Protocol, edit.ProtocolId = 9443, "trojan", "2"
	if err := f.logic.UpdateNode(edit, nil); err != nil {
		t.Fatal(err)
	}
	edit.Protocol = "vless"
	if err := f.logic.UpdateNode(edit, nil); err != nil {
		t.Fatal(err)
	}
	if f.targetPort != 9443 || f.patches != 2 {
		t.Fatal("remote manual target was overwritten")
	}
}

func TestNekoproxyCreatedTargetRespectsManualOverrideAndOfflineSave(t *testing.T) {
	for _, tc := range []struct {
		name            string
		manual, offline bool
		want            int
	}{
		{"auto", false, false, 9443}, {"explicit old default", true, false, 443}, {"sync off", false, true, 443},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupIntegration(t)
			f.seedProtocols(t)
			data := &node.Node{Name: "new", NodeType: "landing", ServerId: 99, Protocol: "vless", ProtocolId: "2"}
			opts := newForwardOptions()
			opts.Create.TargetPortManual = tc.manual
			if err := f.logic.CreateNode(data, opts); err != nil {
				t.Fatal(err)
			}
			data.Protocol = "trojan"
			sync := !tc.offline
			f.unavailable = tc.offline
			if err := f.logic.UpdateNode(data, &sync); err != nil {
				t.Fatal(err)
			}
			if f.targetPort != tc.want || f.client.Node.GetX(t.Context(), data.Id).Protocol != "trojan" {
				t.Fatalf("expected target %d, got %d", tc.want, f.targetPort)
			}
		})
	}
}

func TestNekoproxyConfigMasksTokenAndSeparatesServers(t *testing.T) {
	f := setupIntegration(t)
	config, err := f.logic.Config()
	if err != nil || !config.HasAPIToken {
		t.Fatalf("config: %+v %v", config, err)
	}
	encoded, _ := json.Marshal(config)
	if strings.Contains(string(encoded), "nkp_test") {
		t.Fatal("config exposed the API token")
	}
	if err := f.logic.UpdateConfig(&types.UpdateNekoproxyConfigRequest{APIURL: f.url}); err != nil {
		t.Fatal(err)
	}
	if err := f.logic.UpdateConfig(&types.UpdateNekoproxyConfigRequest{APIURL: "https://other.example.com"}); err == nil {
		t.Fatal("saved token reused with another server")
	}
	f.client.NekoproxyBinding.Create().SetNodeID(1).SetAPIURL("https://other.example.com").SetForwardID("l4s-1").SetSessionID("session-1").SaveX(context.Background())
	result, err := f.logic.Sync(&types.SyncNekoproxyRequest{})
	if err != nil || result.Updated != 0 || f.client.Node.GetX(context.Background(), 1).Address != "old.example.com" {
		t.Fatal("another server's binding was synchronized")
	}
}
