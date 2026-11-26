package updater

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type failingUpdateTransport struct{}

func (failingUpdateTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("network unavailable")
}

func TestUpdaterRejectsBrokenArchivesAndDownloadFailures(t *testing.T) {
	u := NewUpdater()
	dest := t.TempDir()
	if _, err := u.extractZip([]byte("broken"), dest); err == nil {
		t.Fatal("broken zip was accepted")
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create("README")
	require.NoError(t, err)
	_, err = io.WriteString(entry, "docs")
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	if _, err := u.extractZip(archive.Bytes(), dest); err == nil {
		t.Fatal("zip without the server binary was accepted")
	}
	if _, err := u.extractTarGz([]byte("broken"), dest); err == nil {
		t.Fatal("broken tarball was accepted")
	}
	u.HTTPClient.Transport = updateTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("upstream"))}, nil
	})
	release := &Release{TagName: "v1.2.0", Assets: []Asset{{Name: legacyAssetName("v1.2.0"), BrowserDownloadURL: "https://release.example/binary"}}}
	_, err = u.Download(release)
	require.ErrorContains(t, err, "status code 502")
	u.HTTPClient.Transport = failingUpdateTransport{}
	_, err = u.Download(release)
	require.ErrorContains(t, err, "failed to download asset")
}

func TestReleaseUpdateRetriesAgainstAnIsolatedInstallation(t *testing.T) {
	u := NewUpdater()
	u.CurrentVersion = "v1.1.0"
	const next = "v1.2.0"
	const assetURL = "https://releases.example.test/next"
	asset := legacyAssetName(next)
	executable := "ppanel-server"
	if strings.HasSuffix(asset, ".zip") {
		executable += ".exe"
	}
	archive := releaseArchive(t, asset, executable)
	dir := t.TempDir()
	installed := filepath.Join(dir, executable)
	require.NoError(t, os.WriteFile(installed, []byte("previous release"), 0o755))
	var stage string
	var downloads int
	u.HTTPClient.Transport = updateTransport(func(req *http.Request) (*http.Response, error) {
		var body []byte
		status := http.StatusOK
		switch req.URL.String() {
		case githubAPIURL:
			tag := next
			if stage == "current" {
				tag = u.CurrentVersion
			}
			if stage == "offline" {
				status = http.StatusServiceUnavailable
			}
			var err error
			body, err = json.Marshal(Release{TagName: tag, Assets: []Asset{{Name: asset, BrowserDownloadURL: assetURL}}})
			if err != nil {
				return nil, err
			}
		case assetURL:
			downloads++
			body = archive
			if stage == "truncated" {
				body = archive[:4]
			}
		default:
			t.Fatalf("unexpected release URL %s", req.URL)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})
	for _, attempt := range []struct {
		stage, message string
		downloads      int
	}{
		{"current", "latest version", 0}, {"offline", "503", 0}, {"truncated", "failed to download", 1}, {"installation unavailable", "failed to apply", 2}, {"recovered", "", 3},
	} {
		stage = attempt.stage
		var staged string
		err := u.update(func(filename string) error {
			staged = filename
			if stage == "installation unavailable" {
				return errors.New("volume is read-only")
			}
			return u.applyTo(installed, filename)
		})
		want := "previous release"
		if attempt.message != "" {
			require.ErrorContains(t, err, attempt.message)
		} else {
			require.NoError(t, err)
			want = "release content of " + executable
		}
		got, err := os.ReadFile(installed)
		require.NoError(t, err)
		require.Equal(t, want, string(got), stage)
		require.Equal(t, attempt.downloads, downloads, stage)
		if staged != "" {
			require.NoDirExists(t, filepath.Dir(staged))
		}
	}
}

func TestInstallationSurvivesAnIncompleteUpdateAndCanRetry(t *testing.T) {
	dir := t.TempDir()
	installed := filepath.Join(dir, "panel")
	staged := filepath.Join(dir, "staged-panel")
	require.NoError(t, os.WriteFile(installed, []byte("release one"), 0o755))
	u := NewUpdater()
	require.ErrorContains(t, u.applyTo(installed, staged), "failed to replace binary")
	readInstalled := func(want string) {
		t.Helper()
		content, err := os.ReadFile(installed)
		require.NoError(t, err)
		require.Equal(t, want, string(content))
	}
	readInstalled("release one")
	require.NoError(t, os.WriteFile(staged, []byte("release two"), 0o755))
	require.NoError(t, u.applyTo(installed, staged))
	readInstalled("release two")
	require.NoFileExists(t, installed+".backup")
	require.NoFileExists(t, installed+".old")
}

func TestUpdaterCopiesAndReplacesFiles(t *testing.T) {
	u := NewUpdater()
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "destination")
	require.NoError(t, os.WriteFile(source, []byte("new binary"), 0o700))
	require.NoError(t, os.WriteFile(destination, []byte("old binary"), 0o700))
	require.NoError(t, u.replaceFile(source, destination))
	content, err := os.ReadFile(destination)
	require.NoError(t, err)
	require.Equal(t, "new binary", string(content))
	require.Error(t, u.copyFile(filepath.Join(dir, "missing"), destination))
}
