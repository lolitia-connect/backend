package cmd

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"gopkg.in/yaml.v3"
)

func TestHealthURL(t *testing.T) {
	for _, test := range []struct{ host, want string }{
		{"", "http://127.0.0.1:8080/healthz"},
		{"0.0.0.0", "http://127.0.0.1:8080/healthz"},
		{"[::]", "http://127.0.0.1:8080/healthz"},
		{"::1", "http://[::1]:8080/healthz"},
		{"[::1]", "http://[::1]:8080/healthz"},
		{"127.0.0.2", "http://127.0.0.2:8080/healthz"},
	} {
		if got := healthURL(config.File{Host: test.host, Port: 8080}); got != test.want {
			t.Fatalf("host %q: %q, want %q", test.host, got, test.want)
		}
	}
}

func TestHealthcheckStatusTLSAndCancellation(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		tls    bool
	}{
		{"healthy", http.StatusOK, false},
		{"unavailable", http.StatusServiceUnavailable, false},
		{"redirect", http.StatusTemporaryRedirect, false},
		{"TLS", http.StatusOK, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/healthz" {
					t.Errorf("request path = %q", request.URL.Path)
				}
				response.WriteHeader(test.status)
			})
			var server *httptest.Server
			if test.tls {
				server = httptest.NewTLSServer(handler)
			} else {
				server = httptest.NewServer(handler)
			}
			defer server.Close()
			target, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			host, portText, err := net.SplitHostPort(target.Host)
			if err != nil {
				t.Fatal(err)
			}
			port, err := strconv.Atoi(portText)
			if err != nil {
				t.Fatal(err)
			}
			settings := config.File{Host: host, Port: port, TLS: config.TLS{Enable: test.tls}}
			data, err := yaml.Marshal(settings)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "health.yaml")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := healthcheck(context.Background(), path, time.Second); (err == nil) != (test.status == http.StatusOK) {
				t.Fatalf("status %d: %v", test.status, err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := healthcheck(ctx, path, time.Second); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled healthcheck: %v", err)
			}
		})
	}
}
