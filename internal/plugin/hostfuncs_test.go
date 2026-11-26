package plugin

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	pluginv1 "github.com/perfect-panel/server/api/plugin/v1"
	"github.com/stretchr/testify/require"
)

func TestPluginReportUploadRequiresACompleteRemoteResponse(t *testing.T) {
	const endpoint = "http://192.0.2.10:8080/report"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || r.Method != http.MethodPost || r.URL.Path != "/report" || string(body) != "monthly report" || r.Header.Get("X-Report") != "April" {
			http.Error(w, "invalid report", http.StatusBadRequest)
			return
		}
		w.Header().Set("X-Receipt", "42")
		w.Header().Set("Content-Length", "8")
		w.WriteHeader(http.StatusAccepted)
		if r.Header.Get("X-Delivery") == "first" {
			_, _ = io.WriteString(w, "acc")
			return
		}
		_, _ = io.WriteString(w, "accepted")
	}))
	t.Cleanup(upstream.Close)
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "192.0.2.10:8080" {
			return nil, &net.AddrError{Err: "unexpected report destination", Addr: address}
		}
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}
	report := &pluginv1.HttpRequestRequest{Method: http.MethodPost, Url: endpoint, Headers: map[string]string{"X-Report": "April", "X-Delivery": "first"}, Body: []byte("monthly report")}
	receipt, err := doHTTPWithDial(t.Context(), report, dial)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.Nil(t, receipt)
	report.Headers["X-Delivery"] = "retry"
	receipt, err = doHTTPWithDial(t.Context(), report, dial)
	require.NoError(t, err)
	require.EqualValues(t, http.StatusAccepted, receipt.Status)
	require.Equal(t, "42", receipt.Headers["X-Receipt"])
	require.Equal(t, []byte("accepted"), receipt.Body)
}

func TestIsSafeConfigKey(t *testing.T) {
	tests := []struct {
		key  string
		safe bool
	}{
		{"Site.SiteName", true},
		{"Site.SiteDesc", true},
		{"Site.Host", true},
		{"Currency.Unit", true},
		{"Currency.Symbol", true},
		{"Debug", true},
		{"Host", true},
		{"Port", true},
		// Unsafe keys — must be rejected
		{"Database.Addr", false},
		{"Database.Password", false},
		{"Redis.Pass", false},
		{"JwtAuth.AccessSecret", false},
		{"Node.NodeSecret", false},
		{"", false},
		{"unknown.key", false},
	}

	for _, tt := range tests {
		if got := isSafeConfigKey(tt.key); got != tt.safe {
			t.Errorf("isSafeConfigKey(%q) = %v, want %v", tt.key, got, tt.safe)
		}
	}
}

func TestIsURLAllowed(t *testing.T) {
	tests := []struct {
		url     string
		allowed bool
	}{
		{"https://192.0.2.10/", true},
		{"https://[2001:db8::10]/", true},
		{"http://127.0.0.1:3000/test", false},
		{"http://[::1]/", false},
		{"http://10.0.0.1/", false},
		{"ftp://192.0.2.10/", false},
		// Blocked — metadata endpoints
		{"https://metadata.google.internal/", false},
		{"http://169.254.169.254/latest/meta-data", false},
	}

	for _, tt := range tests {
		if got := isURLAllowed(tt.url); got != tt.allowed {
			t.Errorf("isURLAllowed(%q) = %v, want %v", tt.url, got, tt.allowed)
		}
	}
}

func TestSplitParams(t *testing.T) {
	tests := []struct {
		input    string
		n        int
		expected []string
	}{
		{"a|b|c", 3, []string{"a", "b", "c"}},
		{"key|value|3600", 3, []string{"key", "value", "3600"}},
		{"key|value", 3, []string{"key", "value"}},
		{"single", 3, []string{"single"}},
		{"a|b|c|d", 3, []string{"a", "b", "c|d"}},
		{"", 2, []string{""}},
	}

	for _, tt := range tests {
		result := splitParams([]byte(tt.input), tt.n)
		if len(result) != len(tt.expected) {
			t.Errorf("splitParams(%q, %d) len = %d, want %d", tt.input, tt.n, len(result), len(tt.expected))
			continue
		}
		for i, v := range result {
			if v != tt.expected[i] {
				t.Errorf("splitParams(%q, %d)[%d] = %q, want %q", tt.input, tt.n, i, v, tt.expected[i])
			}
		}
	}
}

func TestParseInt64(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		hasError bool
	}{
		{"0", 0, false},
		{"3600", 3600, false},
		{"1234567890", 1234567890, false},
		{"-1", 0, true}, // no sign handling
		{"abc", 0, true},
		{"12a", 12, true}, // parse stops at 'a'
	}

	for _, tt := range tests {
		val, err := parseInt64(tt.input)
		if tt.hasError && err == nil {
			t.Errorf("parseInt64(%q) expected error", tt.input)
		}
		if !tt.hasError && err != nil {
			t.Errorf("parseInt64(%q) unexpected error: %v", tt.input, err)
		}
		if val != tt.expected {
			t.Errorf("parseInt64(%q) = %d, want %d", tt.input, val, tt.expected)
		}
	}
}
