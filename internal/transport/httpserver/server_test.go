package httpserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/test/mock"
	appconfig "github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/tool"
)

type proxyPeerConn struct {
	*mock.Conn
	peer net.Addr
}

func (connection proxyPeerConn) RemoteAddr() net.Addr { return connection.peer }

func TestProxyTrustAppliesToNativeAndCompatibilityHandlers(t *testing.T) {
	for _, test := range []struct {
		name      string
		trusted   []string
		peer      string
		forwarded string
		want      string
	}{
		{"public spoof", nil, "203.0.113.5", "198.51.100.7", "203.0.113.5"},
		{"private proxy", nil, "172.17.0.1", "1.2.3.4, 198.51.100.7, 10.0.0.2", "198.51.100.7"},
		{"direct private client", []string{"none"}, "127.0.0.1", "198.51.100.7", "127.0.0.1"},
		{"explicit proxy", []string{"192.0.2.1"}, "192.0.2.1", "198.51.100.7", "198.51.100.7"},
		{"invalid configuration", []string{"bad-proxy"}, "127.0.0.1", "198.51.100.7", "127.0.0.1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &svc.ServiceContext{Config: appconfig.Config{TrustedProxies: test.trusted}}
			server := New(service, "127.0.0.1:0", nil)
			server.Engine().GET("/client-native", func(_ context.Context, request *app.RequestContext) {
				request.String(http.StatusOK, "%s", request.ClientIP())
			})
			server.Engine().GET("/client-compat", hertzx.Wrap(func(request *hertzx.Context) {
				request.String(http.StatusOK, "%s", request.ClientIP())
			}))
			for _, path := range []string{"/client-native", "/client-compat"} {
				request := server.Engine().NewContext()
				request.SetConn(proxyPeerConn{Conn: mock.NewConn(""), peer: &net.TCPAddr{IP: net.ParseIP(test.peer), Port: 41000}})
				request.Request.SetRequestURI(path)
				request.Request.Header.SetMethod(http.MethodGet)
				request.Request.Header.Set("X-Forwarded-For", test.forwarded)
				server.Engine().ServeHTTP(context.Background(), request)
				if got := string(request.Response.Body()); got != test.want {
					t.Fatalf("%s client IP = %q, want %q", path, got, test.want)
				}
			}
		})
	}
	if _, err := ParseTrustedProxies([]string{"none", "10.0.0.0/8"}); err == nil {
		t.Fatal("none combined with a proxy was accepted")
	}
}

func TestConfiguredCorsRequiresExactOrigin(t *testing.T) {
	server := New(&svc.ServiceContext{Config: appconfig.Config{AllowedOrigins: []string{"https://Panel.Example/"}}}, "127.0.0.1:0", nil)
	for _, origin := range []string{"https://panel.example", "https://panel.example.evil", "https://other.example", "null"} {
		request := server.Engine().NewContext()
		request.Request.SetRequestURI("/v1/server/online")
		request.Request.Header.SetMethod(http.MethodOptions)
		request.Request.Header.Set("Origin", origin)
		server.Engine().ServeHTTP(context.Background(), request)
		allowed := string(request.Response.Header.Peek("Access-Control-Allow-Origin"))
		if (allowed == origin) != (origin == "https://panel.example") || request.Response.StatusCode() != http.StatusNoContent {
			t.Fatalf("origin %q: allowed=%q, status=%d", origin, allowed, request.Response.StatusCode())
		}
		if string(request.Response.Header.Peek("Vary")) != "Origin" {
			t.Fatal("origin-dependent CORS response is cacheable across origins")
		}
	}
}

func TestHealthDoesNotDependOnReadiness(t *testing.T) {
	app := newTestServer("secret")
	status, body := performNativeRequest(app, http.MethodGet, HealthzPath)
	if status != http.StatusOK || !strings.Contains(body, `"status":"ok"`) {
		t.Fatalf("liveness: %d %s", status, body)
	}
	status, body = performNativeRequest(app, http.MethodGet, ReadyzPath)
	if status != http.StatusServiceUnavailable || !strings.Contains(body, "database unavailable") {
		t.Fatalf("readiness without dependencies: %d %s", status, body)
	}
}

func TestListenerBindFailureReturnsError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := New(&svc.ServiceContext{}, listener.Addr().String(), nil)
	if err := server.Run(); err == nil {
		t.Fatal("occupied listener was reported as a successful start")
	}
}

func TestShutdownBeforeRunPreventsListening(t *testing.T) {
	server := New(&svc.ServiceContext{}, "127.0.0.1:0", nil)
	_ = server.Shutdown(context.Background())
	if err := server.Run(); err != nil {
		t.Fatal(err)
	}
	if server.Engine().IsRunning() {
		t.Fatal("stopped server started listening")
	}
}

func TestShutdownDuringStartupPreventsListening(t *testing.T) {
	server := New(&svc.ServiceContext{}, "127.0.0.1:0", nil)
	entered, release := make(chan struct{}), make(chan struct{})
	server.Engine().OnRun = append(server.Engine().OnRun, func(context.Context) error {
		close(entered)
		<-release
		return nil
	})
	result := make(chan error, 1)
	go func() { result <- server.Run() }()
	<-entered
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown during startup left a running listener")
	}
}

func TestReadinessCachesBoundedDependencyProbes(t *testing.T) {
	calls := 0
	failure := errors.New("database://private-credentials")
	checker := &healthChecker{probes: []healthProbe{{name: "database", ping: func(ctx context.Context) error {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Second {
			t.Fatal("probe has no bounded deadline")
		}
		return failure
	}}}}
	for attempt := 0; attempt < 2; attempt++ {
		if reason := checker.notReady(context.Background()); reason != "database unreachable" {
			t.Fatalf("probe reason exposed an error: %q", reason)
		}
	}
	if calls != 1 {
		t.Fatalf("cached probe calls = %d, want 1", calls)
	}
	failure = nil
	checker.checkedAt = time.Now().Add(-6 * time.Second)
	if reason := checker.notReady(context.Background()); reason != "" || calls != 2 {
		t.Fatalf("readiness did not recover: %q, calls=%d", reason, calls)
	}
}

func TestServerSecretMiddlewareBlocksMigratedPost(t *testing.T) {
	app := newTestServer("secret")

	status, body := performNativeRequest(app, http.MethodPost, "/v1/server/online?secret_key=wrong")
	if status != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, status)
	}
	if body != "Forbidden" {
		t.Fatalf("expected forbidden body, got %q", body)
	}
}

func TestQueryServerProtocolConfigRejectsInvalidID(t *testing.T) {
	app := newTestServer("secret")

	status, body := performNativeRequest(app, http.MethodGet, "/v2/server/not-a-number?secret_key=secret")
	if status != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, status)
	}
	if body != "Invalid Params" {
		t.Fatalf("expected invalid params body, got %q", body)
	}
}

func TestQueryServerProtocolConfigRejectsInvalidSecret(t *testing.T) {
	app := newTestServer("secret")

	status, body := performNativeRequest(app, http.MethodGet, "/v2/server/1?secret_key=wrong")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, status)
	}
	if body != "Unauthorized" {
		t.Fatalf("expected unauthorized body, got %q", body)
	}
}

func TestCorsPreflightBypassesServerSecretMiddleware(t *testing.T) {
	app := newTestServer("secret")

	ctx := app.Engine().NewContext()
	ctx.Request.SetRequestURI("/v1/server/online")
	ctx.Request.Header.SetMethod(http.MethodOptions)
	ctx.Request.Header.Set("Origin", "https://example.com")
	app.Engine().ServeHTTP(context.Background(), ctx)

	if status := ctx.Response.StatusCode(); status != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, status)
	}
	if origin := string(ctx.Response.Header.Peek("Access-Control-Allow-Origin")); origin != "https://example.com" {
		t.Fatalf("expected CORS origin header, got %q", origin)
	}
}

// An installation whose node secret has not been provisioned yet must not
// authenticate anyone; a bare `?secret_key=` used to compare equal to it.
func TestServerSecretMiddlewareRejectsUnprovisionedSecret(t *testing.T) {
	app := newTestServer("")

	status, body := performNativeRequest(app, http.MethodPost, "/v1/server/online?secret_key=")
	if status != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, status)
	}
	if body != "Forbidden" {
		t.Fatalf("expected forbidden body, got %q", body)
	}
}

func TestQueryServerProtocolConfigRejectsUnprovisionedSecret(t *testing.T) {
	app := newTestServer("")

	status, body := performNativeRequest(app, http.MethodGet, "/v2/server/1?secret_key=")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, status)
	}
	if body != "Unauthorized" {
		t.Fatalf("expected unauthorized body, got %q", body)
	}
}

// The well-known default the seed data used to ship is public knowledge, so a
// deployment still carrying it has to be reported at boot; provisioning must
// never rotate it silently, because every node was configured with that value.
func TestLegacyDefaultNodeSecretIsRecognised(t *testing.T) {
	if tool.LegacyDefaultNodeSecret != "12345678" {
		t.Fatalf("legacy default = %q, want the seeded value so the boot check still matches", tool.LegacyDefaultNodeSecret)
	}
}

func newTestServer(secret string) *Server {
	return New(&svc.ServiceContext{
		Config: appconfig.Config{
			Node: appconfig.NodeConfig{
				NodeSecret: secret,
			},
		},
	}, "127.0.0.1:0", nil)
}

func performNativeRequest(server *Server, method, uri string) (int, string) {
	ctx := server.Engine().NewContext()
	ctx.Request.SetRequestURI(uri)
	ctx.Request.Header.SetMethod(method)
	server.Engine().ServeHTTP(context.Background(), ctx)
	return ctx.Response.StatusCode(), string(ctx.Response.Body())
}
