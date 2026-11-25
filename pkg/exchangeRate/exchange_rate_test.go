package exchangeRate

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/require"
)

func TestConvertUsesProviderQuoteAndPropagatesFailures(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		status      int
		want        float64
	}{
		{"converted", `{"success":true,"result":19.875}`, http.StatusOK, 19.875},
		{"unsupported currency pair", `{"success":false,"result":123}`, http.StatusOK, 0},
		{"invalid api key", `{"message":"invalid api key"}`, http.StatusUnauthorized, 0},
		{"truncated response", `{"success":true,`, http.StatusOK, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/currency_data/convert" || r.Header.Get("apikey") != "local-key" {
					t.Errorf("unexpected conversion request %s %v", r.Method, r.URL)
				}
				wantQuery := url.Values{"from": {"USD"}, "to": {"EUR"}, "amount": {"21.375"}}
				if got := r.URL.Query().Encode(); got != wantQuery.Encode() {
					t.Errorf("query = %s, want %s", got, wantQuery.Encode())
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.reply)
			}))
			t.Cleanup(server.Close)
			result, err := convert(resty.NewWithClient(server.Client()).SetBaseURL(server.URL), "USD", "EUR", "local-key", 21.375)
			if tc.want == 0 {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.want, result)
		})
	}
}
