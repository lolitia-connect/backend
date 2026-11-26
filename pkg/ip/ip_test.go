package ip

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"io"
	"net/http"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

type geoTransport func(*http.Request) (*http.Response, error)

func (f geoTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRegionLookupDecodesAdvertisedFormatsForTheNodeMap(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	for _, encoding := range []string{"deflate", "br", "zstd"} {
		t.Run(encoding, func(t *testing.T) {
			var compressed bytes.Buffer
			var writer io.WriteCloser
			switch encoding {
			case "deflate":
				writer = zlib.NewWriter(&compressed)
			case "br":
				writer = brotli.NewWriter(&compressed)
			case "zstd":
				var err error
				writer, err = zstd.NewWriter(&compressed, zstd.WithEncoderConcurrency(1))
				require.NoError(t, err)
			}
			_, err := io.WriteString(writer, `{"country_name":"Canada","city":"Toronto","loc":"43.65,-79.38"}`)
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			http.DefaultTransport = geoTransport(func(req *http.Request) (*http.Response, error) {
				require.Contains(t, req.Header.Get("Accept-Encoding"), encoding)
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Encoding": {encoding}}, Body: io.NopCloser(bytes.NewReader(compressed.Bytes()))}, nil
			})
			got, err := GetRegionByIp("192.0.2.34")
			require.NoError(t, err)
			require.Equal(t, "Toronto", got.City)
			require.Equal(t, []string{"43.65", "-79.38"}, []string{got.Latitude, got.Longitude})
			require.Equal(t, []string{"45.4215", "-75.6972"}, []string{got.LatitudeCenter, got.LongitudeCenter})
		})
	}
}

func TestMapCentersResolveCountryAndRegionLabels(t *testing.T) {
	for _, place := range []struct {
		name, latitude, longitude string
	}{
		{"CA", "38.9072", "-77.0369"},
		{" CA , Toronto ", "45.4215", "-75.6972"},
		{"british columbia", "45.4215", "-75.6972"},
		{"Ontario, Toronto", "45.4215", "-75.6972"},
		{"Japan", "35.6762", "139.6503"},
		{"jp", "35.6762", "139.6503"},
		{"United States of America", "38.9072", "-77.0369"},
		{"Province of Ontario", "45.4215", "-75.6972"},
	} {
		t.Run(place.name, func(t *testing.T) {
			latitude, longitude, ok := GetCapitalCoordinates(place.name)
			require.True(t, ok)
			require.Equal(t, place.latitude, latitude)
			require.Equal(t, place.longitude, longitude)
		})
	}
	for _, unknown := range []string{" ", "Exampleland", ", Unlisted City", "Canada, Ontario, Toronto"} {
		latitude, longitude, ok := GetCapitalCoordinates(unknown)
		require.False(t, ok, "unexpected center for %q", unknown)
		require.Empty(t, latitude)
		require.Empty(t, longitude)
	}
}

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
