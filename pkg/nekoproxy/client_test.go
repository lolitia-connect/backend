package nekoproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTokenClientUsesOnlyPublicAPIAndPagesResults(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxy/api/v1/l4/services" || r.Header.Get("Authorization") != "Bearer nkp_test" {
			t.Errorf("unexpected request path or authentication")
		}
		calls++
		rows := []Service{{ID: "last"}}
		if r.URL.Query().Get("offset") == "0" {
			rows = make([]Service, 500)
		}
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer server.Close()
	client, err := New(server.URL+"/proxy/api/v1/l4/", "nkp_test")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := client.Services(context.Background())
	if err != nil || len(rows) != 501 || calls != 2 {
		t.Fatalf("pagination: rows=%d calls=%d error=%v", len(rows), calls, err)
	}
}

func TestTokenClientRejectsRedirectsAndRedactsRemoteErrors(t *testing.T) {
	redirected := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected = true }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	client, _ := New(server.URL, "nkp_secret")
	if err := client.Check(context.Background()); err == nil || redirected || strings.Contains(err.Error(), "nkp_secret") {
		t.Fatalf("unsafe redirect handling: %v", err)
	}
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"nkp_secret from upstream"}`)
	})
	if err := client.Check(context.Background()); err == nil || strings.Contains(err.Error(), "nkp_secret") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestSessionEndpointRejectsInactiveAndWildcardAndPreservesIPv6(t *testing.T) {
	valid := Session{Status: "active", ListenAddr: "[2001:db8::1]:1234", AllocatedPort: 1234}
	host, port, err := valid.Endpoint()
	if err != nil || host != "2001:db8::1" || port != 1234 {
		t.Fatalf("IPv6 endpoint: %q %d %v", host, port, err)
	}
	for _, session := range []Session{
		{Status: "ended", NodeHost: "entry.example.com", AllocatedPort: 80},
		{Status: "active", NodeHost: "0.0.0.0", AllocatedPort: 80},
		{Status: "active", NodeHost: "::", AllocatedPort: 80},
		{Status: "active", NodeHost: "entry.example.com", AllocatedPort: 65536},
		{Status: "active", ListenAddr: "entry.example.com:80", AllocatedPort: 81},
	} {
		if _, _, err := session.Endpoint(); err == nil {
			t.Errorf("accepted unusable session %+v", session)
		}
	}
}
