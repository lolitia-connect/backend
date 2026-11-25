package nekoproxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
)

func logicWithConnection(t *testing.T, client *ent.Client, baseURL string) *Logic {
	t.Helper()
	store := repository.NewStore(client, nil)
	payload, err := json.Marshal(connection{APIURL: baseURL, Token: "nkp_original"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.System().UpsertByCategoryKey(t.Context(), "nekoproxy", configKey, string(payload), "string", ""); err != nil {
		t.Fatal(err)
	}
	return NewLogic(t.Context(), &svc.ServiceContext{Store: store, Ent: client})
}

func registerInventoryRoute(t *testing.T, mux *http.ServeMux, path, query string, status int, body string) {
	t.Helper()
	mux.HandleFunc("GET /api/v1/l4"+path, func(w http.ResponseWriter, r *http.Request) {
		if token := r.Header.Get("Authorization"); token != "Bearer nkp_original" || r.URL.RawQuery != query {
			t.Errorf("unexpected request for %s: authorization = %q, query = %q", path, token, r.URL.RawQuery)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	})
}

func TestNekoproxyStateJoinsRemoteInventoryWithLocalBindings(t *testing.T) {
	for _, failedRoute := range []string{"", "/sessions", "/chains", "/entry-groups"} {
		name := "complete inventory"
		if failedRoute != "" {
			name = strings.TrimPrefix(failedRoute, "/") + " unavailable"
		}
		t.Run(name, func(t *testing.T) {
			mux := http.NewServeMux()
			for path, body := range map[string]string{
				"/services":     `[{"id":"live","remark":"Public","chainId":"chain-1","chainName":"Primary","protocol":"tcp_udp","targetHost":"origin.example.test","targetPort":443,"enabled":true},{"id":"blocked","chainDisabled":true,"enabled":true,"paused":true}]`,
				"/sessions":     `{"rows":[{"sessionId":"orphan","serviceId":"deleted","nodeHost":"entry.example.test","allocatedPort":2222,"status":"active"},{"sessionId":"entry","serviceId":"live","listenAddr":"[2001:db8::1]:2443","allocatedPort":2443,"entryGroupId":"pool","status":"active"}]}`,
				"/chains":       `[{"id":"chain-1","name":"Primary"}]`,
				"/entry-groups": `[{"id":"pool","name":"Public ports","listenHost":"entry.example.test","portStart":2000,"portEnd":3000}]`,
			} {
				status := http.StatusOK
				if path == failedRoute {
					status, body = http.StatusServiceUnavailable, `{"error":"INVENTORY_UNAVAILABLE"}`
				}
				registerInventoryRoute(t, mux, path, "limit=500&offset=0", status, body)
			}
			server := httptest.NewServer(mux)
			t.Cleanup(server.Close)
			client := newIntegrationClient(t)
			logic := logicWithConnection(t, client, server.URL)
			for _, id := range []int64{2, 1} {
				client.Node.Create().SetID(id).SaveX(t.Context())
			}
			client.NekoproxyBinding.Create().SetNodeID(2).SetAPIURL("https://previous.example.test").SetForwardID("previous").SetSessionID("previous-entry").SaveX(t.Context())
			client.NekoproxyBinding.Create().SetNodeID(1).SetAPIURL(server.URL).SetForwardID("live").SetSessionID("entry").SetSyncOnSave(false).SaveX(t.Context())

			want := &types.NekoproxyState{
				APIURL: server.URL,
				Bindings: []types.NekoproxyBinding{
					{NodeID: 1, APIURL: server.URL, ForwardID: "live", SessionID: "entry"},
					{NodeID: 2, APIURL: "https://previous.example.test", ForwardID: "previous", SessionID: "previous-entry", SyncOnSave: true},
				},
				Forwards: []types.NekoproxyForward{},
				Chains:   []types.NekoproxyChain{},
				Groups:   []types.NekoproxyEntryGroup{},
			}
			if failedRoute != "" {
				want.Error = "Nekoproxy HTTP 503: INVENTORY_UNAVAILABLE"
			} else {
				want.Forwards = []types.NekoproxyForward{
					{ID: "live", Name: "Public", ChainID: "chain-1", ChainName: "Primary", Protocol: "tcp_udp", TargetHost: "origin.example.test", TargetPort: 443, Enabled: true,
						Sessions: []types.NekoproxySession{{ID: "entry", Address: "2001:db8::1", Port: 2443, Status: "active", GroupID: "pool"}}},
					{ID: "blocked", Paused: true, Sessions: []types.NekoproxySession{}},
				}
				want.Chains = []types.NekoproxyChain{{ID: "chain-1", Name: "Primary"}}
				want.Groups = []types.NekoproxyEntryGroup{{ID: "pool", Name: "Public ports", ListenHost: "entry.example.test", PortStart: 2000, PortEnd: 3000}}
			}
			got, err := logic.State()
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("State() = %+v, %v; want %+v", got, err, want)
			}
		})
	}
}

func TestNekoproxyRebindPreservesOrClearsManagedPort(t *testing.T) {
	mux := http.NewServeMux()
	registerInventoryRoute(t, mux, "/services/current", "", http.StatusOK, `{"id":"current","enabled":true}`)
	registerInventoryRoute(t, mux, "/services/replacement", "", http.StatusOK, `{"id":"replacement","enabled":true}`)
	registerInventoryRoute(t, mux, "/services/paused", "", http.StatusOK, `{"id":"paused","enabled":true,"paused":true}`)
	registerInventoryRoute(t, mux, "/sessions", "limit=500&offset=0", http.StatusOK, `{"rows":[
		{"sessionId":"renewed","serviceId":"current","nodeHost":"renewed.example.test","allocatedPort":2443,"status":"active"},
		{"sessionId":"new-entry","serviceId":"replacement","nodeHost":"replacement.example.test","allocatedPort":3443,"status":"active"},
		{"sessionId":"paused-entry","serviceId":"paused","nodeHost":"paused.example.test","allocatedPort":4443,"status":"active"}
	]}`)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	ctx := t.Context()
	client := newIntegrationClient(t)
	logic := logicWithConnection(t, client, server.URL)
	node := client.Node.Create().SetName("original node").SetProtocol("vless").SetProtocolID("2").SaveX(ctx)
	original := client.NekoproxyBinding.Create().SetNodeID(node.ID).SetAPIURL(server.URL).SetForwardID("current").SetSessionID("original").SetManagedTargetPort(443).SetSyncOnSave(false).SaveX(ctx)

	assertStored := func(forward, session, address string, port, managed uint16) {
		t.Helper()
		binding := client.NekoproxyBinding.Query().OnlyX(ctx)
		if binding.ID != original.ID || binding.NodeID != node.ID || binding.APIURL != server.URL || binding.SyncOnSave || binding.ForwardID != forward || binding.SessionID != session {
			t.Fatalf("persisted binding = %+v, want original binding for %s / %s with sync disabled", binding, forward, session)
		}
		if managed == 0 && binding.ManagedTargetPort != nil || managed != 0 && (binding.ManagedTargetPort == nil || *binding.ManagedTargetPort != managed) {
			t.Fatalf("persisted managed target = %v, want %d (0 means unset)", binding.ManagedTargetPort, managed)
		}
		stored := client.Node.GetX(ctx, node.ID)
		if stored.Address != address || stored.Port != port || stored.Name != node.Name || stored.Protocol != node.Protocol || stored.ProtocolID != node.ProtocolID {
			t.Fatalf("persisted node = %+v, want endpoint %s:%d and original metadata", stored, address, port)
		}
	}
	bind := func(forward, session string) error {
		return logic.Bind(&types.BindNekoproxyRequest{NodeID: node.ID, ForwardID: forward, SessionID: session})
	}
	if err := bind("current", "renewed"); err != nil {
		t.Fatal(err)
	}
	assertStored("current", "renewed", "renewed.example.test", 2443, 443)

	if err := bind("replacement", "renewed"); err == nil || !strings.Contains(err.Error(), "session no longer exists") {
		t.Fatalf("binding a session from another forward returned %v", err)
	}
	assertStored("current", "renewed", "renewed.example.test", 2443, 443)
	if err := bind("paused", "paused-entry"); err == nil || !strings.Contains(err.Error(), "disabled or paused") {
		t.Fatalf("binding a paused forward returned %v", err)
	}
	assertStored("current", "renewed", "renewed.example.test", 2443, 443)

	if err := bind("replacement", "new-entry"); err != nil {
		t.Fatal(err)
	}
	assertStored("replacement", "new-entry", "replacement.example.test", 3443, 0)
}

func TestNekoproxyConfigPersistsOnlyValidatedConnection(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /replacement/api/v1/l4/services", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "limit=1&offset=0" {
			t.Errorf("unexpected validation query: %q", r.URL.RawQuery)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "Bearer nkp_replacement" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":"API_TOKEN_REQUIRED"}`)
			return
		}
		fmt.Fprint(w, `[]`)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	originalURL, replacementURL := server.URL+"/original", server.URL+"/replacement"
	logic := logicWithConnection(t, newIntegrationClient(t), originalURL)
	assertStored := func(want connection) {
		t.Helper()
		row, err := logic.svc.Store.System().FindByCategoryKey(t.Context(), "nekoproxy", configKey)
		if err != nil {
			t.Fatal(err)
		}
		var got connection
		if err := json.Unmarshal([]byte(row.Value), &got); err != nil || got != want {
			t.Fatalf("persisted connection = %+v, %v; want %+v", got, err, want)
		}
	}

	req := &types.UpdateNekoproxyConfigRequest{APIURL: replacementURL + "/api/v1/l4/", APIToken: "nkp_rejected"}
	if err := logic.UpdateConfig(req); err == nil || !strings.Contains(err.Error(), "HTTP 401: API_TOKEN_REQUIRED") {
		t.Fatalf("rejected connection returned %v", err)
	}
	assertStored(connection{APIURL: originalURL, Token: "nkp_original"})

	req.APIToken = " nkp_replacement "
	if err := logic.UpdateConfig(req); err != nil {
		t.Fatal(err)
	}
	assertStored(connection{APIURL: replacementURL, Token: "nkp_replacement"})
}
