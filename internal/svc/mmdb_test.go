package svc

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/pkg/requestmeta"
)

func TestGeoIPOptionalAndRequiredModes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.mmdb")
	location, err := OpenIPLocation(config.GeoIPConfig{Path: path, Download: false})
	if err != nil || location == nil || location.DB != nil {
		t.Fatalf("optional missing GeoIP: %+v, %v", location, err)
	}
	if _, err := OpenIPLocation(config.GeoIPConfig{Path: path, Download: false, Required: true}); err == nil {
		t.Fatal("required missing GeoIP was accepted")
	}
	if err := verifyGeoIPChecksum(path, "invalid"); err == nil {
		t.Fatal("invalid checksum was accepted")
	}
	data := []byte("GeoIP fixture")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if err := verifyGeoIPChecksum(path, hex.EncodeToString(digest[:])); err != nil {
		t.Fatalf("matching checksum rejected: %v", err)
	}
	otherDigest := sha256.Sum256([]byte("different"))
	if err := verifyGeoIPChecksum(path, hex.EncodeToString(otherDigest[:])); err == nil {
		t.Fatal("mismatched checksum was accepted")
	}
}

func TestUnavailableGeoIPIsSafe(t *testing.T) {
	metadata := requestmeta.Metadata{ClientIP: "127.0.0.1"}
	for _, location := range []*IPLocation{nil, {}} {
		if got := location.EnrichMetadata(metadata); got != metadata {
			t.Fatalf("unavailable GeoIP changed metadata: %+v", got)
		}
		if err := location.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGeoIPDownloadPreservesExistingFileOnFailure(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.WriteHeader(status)
				_, _ = response.Write([]byte("invalid database"))
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "city.mmdb")
			if err := os.WriteFile(path, []byte("existing database"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := DownloadGeoIPDatabase(server.URL, path); err == nil {
				t.Fatal("invalid GeoIP download succeeded")
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "existing database" {
				t.Fatalf("existing database changed: %q, %v", data, err)
			}
		})
	}
}
