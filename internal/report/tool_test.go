package report

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayRegistersLoopbackServiceAndHeartbeat(t *testing.T) {
	module := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/common/heartbeat" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(module.Close)
	const secret = "registration-fixture"
	t.Setenv("SECRET_KEY", secret)
	t.Setenv("GATEWAY_MODE", "true")
	for _, tc := range []struct {
		name, reply, wantError string
		status                 int
	}{
		{"unavailable", `{}`, "503", http.StatusServiceUnavailable},
		{"declined", `{"message":"maintenance","data":{"success":false}}`, "maintenance", http.StatusOK},
		{"accepted", `{"data":{"success":true}}`, "", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan RegisterServiceRequest, 1)
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/basic/register" {
					http.NotFound(w, r)
					return
				}
				var registration RegisterServiceRequest
				if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				requests <- registration
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.reply))
			}))
			t.Cleanup(gateway.Close)
			t.Setenv("GATEWAY_PORT", strconv.Itoa(gateway.Listener.Addr().(*net.TCPAddr).Port))
			require.True(t, IsGatewayMode())
			err := RegisterModule(module.Listener.Addr().(*net.TCPAddr).Port)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
			}
			select {
			case registration := <-requests:
				require.Equal(t, secret, registration.Secret)
				require.Equal(t, "/api", registration.ProxyPath)
				require.Equal(t, module.URL, registration.ServiceURL)
				require.Equal(t, module.URL+"/v1/common/heartbeat", registration.HeartbeatURL)
				if tc.wantError == "" {
					resp, err := module.Client().Get(registration.HeartbeatURL)
					require.NoError(t, err)
					defer resp.Body.Close()
					require.Equal(t, http.StatusNoContent, resp.StatusCode)
				}
			default:
				t.Fatal("gateway did not receive a registration")
			}
		})
	}
}

func TestFreePort(t *testing.T) {
	port, err := FreePort()
	if err != nil {
		t.Fatalf("FreePort() error: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Fatalf("FreePort() returned invalid port %d", port)
	}
}

func TestModulePort(t *testing.T) {
	t.Setenv("PPANEL_PORT", "41234")
	port, err := ModulePort()
	if err != nil || port != 41234 {
		t.Fatalf("configured ModulePort() = %d, %v", port, err)
	}

	t.Setenv("PPANEL_PORT", "invalid")
	port, err = ModulePort()
	if err != nil || port <= 0 || port > 65535 {
		t.Fatalf("fallback ModulePort() = %d, %v", port, err)
	}
}
