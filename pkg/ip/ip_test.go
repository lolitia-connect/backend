package ip

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"testing"
)

type geoTransport func(*http.Request) (*http.Response, error)

func (f geoTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRegionLookupFallsBackAcrossGeolocationProviders(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	const address = "192.0.2.33"
	paths := map[string]string{
		"ipinfo.io":      "/" + address + "/json",
		"ipapi.co":       "/" + address + "/json",
		"api.ipbase.com": "/v1/json/" + address,
		"ipwhois.app":    "/json/" + address,
	}
	for _, test := range []struct {
		name, payload, country, city, lat, lon, centerLat, centerLon string
	}{
		{"known country", `{"country_name":"Japan","city":"Osaka","loc":"34.69,135.50"}`, "Japan", "Osaka", "34.69", "135.50", "35.6762", "139.6503"},
		{"unmapped country", `{"country":"Exampleland","city":"Port","latitude":"12.3","longitude":"45.6"}`, "Exampleland", "Port", "12.3", "45.6", "12.3", "45.6"},
		{"providers unavailable", "{}", "", "", "", "", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			seen := make(map[string]bool)
			http.DefaultTransport = geoTransport(func(req *http.Request) (*http.Response, error) {
				host := req.URL.Hostname()
				if seen[host] || paths[host] != req.URL.Path || req.Method != http.MethodGet {
					t.Fatalf("unexpected provider request: %s %s", req.Method, req.URL)
				}
				seen[host] = true
				body := test.payload
				if len(seen) == 1 {
					body = `{`
				} else if len(seen) == 2 {
					body = `{"error":"quota exceeded"}`
				}
				var compressed bytes.Buffer
				w := gzip.NewWriter(&compressed)
				if _, err := io.WriteString(w, body); err != nil {
					t.Fatal(err)
				}
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Encoding": {"gzip"}}, Body: io.NopCloser(&compressed)}, nil
			})
			got, err := GetRegionByIp(address)
			if test.country == "" {
				if err == nil || got != nil || len(seen) != len(paths) {
					t.Fatalf("all providers unavailable: got %+v, err %v after %d providers", got, err, len(seen))
				}
				return
			}
			if err != nil || got == nil || len(seen) != 3 {
				t.Fatalf("lookup: got %+v, err %v after %d providers", got, err, len(seen))
			}
			if got.Country != test.country || got.City != test.city || got.Latitude != test.lat || got.Longitude != test.lon || got.LatitudeCenter != test.centerLat || got.LongitudeCenter != test.centerLon {
				t.Fatalf("provider location and map center = %+v", got)
			}
		})
	}
}
