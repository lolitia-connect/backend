package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type updateTransport func(*http.Request) (*http.Response, error)

func (f updateTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCheckForUpdatesHandlesReleaseAndProviderErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantError string
		status                int
		wantTag               string
		wantUpdate            bool
	}{
		{"new release", `{"tag_name":"v1.2.0"}`, "", 200, "v1.2.0", true},
		{"already installed", `{"tag_name":"1.1.0"}`, "", 200, "1.1.0", false},
		{"draft", `{"tag_name":"v1.2.0","draft":true}`, "", 200, "", false},
		{"prerelease", `{"tag_name":"v1.2.0-rc1","prerelease":true}`, "", 200, "", false},
		{"rate limited", `{}`, "429", 429, "", false},
		{"malformed response", `{`, "failed to decode response", 200, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := NewUpdater()
			u.CurrentVersion = "v1.1.0"
			u.HTTPClient.Transport = updateTransport(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, http.MethodGet, req.Method)
				require.Equal(t, githubAPIURL, req.URL.String())
				require.Equal(t, "application/vnd.github.v3+json", req.Header.Get("Accept"))
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			release, available, err := u.CheckForUpdates()
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantUpdate, available)
			if tc.wantTag == "" {
				require.Nil(t, release)
			} else {
				require.NotNil(t, release)
				require.Equal(t, tc.wantTag, release.TagName)
			}
		})
	}
}

func releaseArchive(t *testing.T, asset string, files ...string) []byte {
	t.Helper()
	var archive bytes.Buffer
	if strings.HasSuffix(asset, ".zip") {
		writer := zip.NewWriter(&archive)
		for _, file := range files {
			entry, err := writer.Create(file)
			require.NoError(t, err)
			_, err = io.WriteString(entry, "release content of "+file)
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())
	} else {
		compressed := gzip.NewWriter(&archive)
		writer := tar.NewWriter(compressed)
		for _, file := range files {
			body := "release content of " + file
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: file, Mode: 0o755, Size: int64(len(body))}))
			_, err := io.WriteString(writer, body)
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())
		require.NoError(t, compressed.Close())
	}
	return archive.Bytes()
}

func TestDownloadSelectsPlatformAssetAndExtractsBinary(t *testing.T) {
	u := NewUpdater()
	u.CurrentVersion = "v1.1.0"
	const targetVersion = "v1.2.0"
	name := legacyAssetName(targetVersion)
	executable := "ppanel-server"
	if strings.HasSuffix(name, ".zip") {
		executable += ".exe"
	}
	archive := releaseArchive(t, name, "README", "nested/"+executable, executable)
	const assetURL = "https://releases.example.test/platform"
	var requested []string
	u.HTTPClient.Transport = updateTransport(func(req *http.Request) (*http.Response, error) {
		requested = append(requested, req.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(archive))}, nil
	})
	_, err := u.Download(&Release{Assets: []Asset{{Name: "checksums.txt"}}})
	require.ErrorContains(t, err, "no suitable asset")
	require.Empty(t, requested)
	binary, err := u.Download(&Release{TagName: targetVersion, Assets: []Asset{
		{Name: "checksums.txt", BrowserDownloadURL: "https://releases.example.test/checksums"},
		{Name: legacyAssetName(u.CurrentVersion), BrowserDownloadURL: "https://releases.example.test/old"},
		{Name: name, BrowserDownloadURL: assetURL},
	}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(binary)) })
	require.Equal(t, []string{assetURL}, requested)
	require.Equal(t, executable, filepath.Base(binary))
	data, err := os.ReadFile(binary)
	require.NoError(t, err)
	require.Equal(t, "release content of "+executable, string(data))
}

func TestDownloadReadsThePlatformArchivePublishedByReleaseCI(t *testing.T) {
	u := NewUpdater()
	u.CurrentVersion = "v1.1.0"
	platform := "ppanel-server-" + runtime.GOOS + "-" + runtime.GOARCH
	ext, executable := ".tar.gz", "ppanel-server"
	if runtime.GOOS == "windows" {
		ext, executable = ".zip", "ppanel-server.exe"
	}
	asset := platform + ext
	packaged := platform + "/" + executable
	archive := releaseArchive(t, asset, platform+"/LICENSE", "other-platform/"+executable, packaged)
	const origin = "https://releases.example.test/v1.2.0/"
	u.HTTPClient.Transport = updateTransport(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, origin+asset, req.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(archive))}, nil
	})
	binary, err := u.Download(&Release{TagName: "v1.2.0", Assets: []Asset{
		{Name: asset + ".sha256", BrowserDownloadURL: origin + asset + ".sha256"},
		{Name: "ppanel-server-other-platform.zip", BrowserDownloadURL: origin + "other.zip"},
		{Name: asset, BrowserDownloadURL: origin + asset},
	}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(binary)) })
	require.Equal(t, executable, filepath.Base(binary))
	data, err := os.ReadFile(binary)
	require.NoError(t, err)
	require.Equal(t, "release content of "+packaged, string(data))
}

func TestNewUpdater(t *testing.T) {
	u := NewUpdater()
	assert.NotNil(t, u)
	assert.Equal(t, "OmnTeam", u.Owner)
	assert.Equal(t, "server", u.Repo)
	assert.NotNil(t, u.HTTPClient)
}

func TestCompareVersions(t *testing.T) {
	u := NewUpdater()

	tests := []struct {
		name           string
		newVersion     string
		currentVersion string
		expected       bool
	}{
		{
			name:           "same version",
			newVersion:     "v1.0.0",
			currentVersion: "v1.0.0",
			expected:       false,
		},
		{
			name:           "different version",
			newVersion:     "v1.1.0",
			currentVersion: "v1.0.0",
			expected:       true,
		},
		{
			name:           "unknown current version",
			newVersion:     "v1.0.0",
			currentVersion: "unknown version",
			expected:       true,
		},
		{
			name:           "version without v prefix",
			newVersion:     "1.1.0",
			currentVersion: "1.0.0",
			expected:       true,
		},
		{
			name:           "empty current version",
			newVersion:     "v1.0.0",
			currentVersion: "",
			expected:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := u.compareVersions(tt.newVersion, tt.currentVersion)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLegacyReleasePackagesExtractOnlyTheRootExecutable(t *testing.T) {
	for _, tc := range []struct{ asset, binary string }{
		{"ppanel-server-v1.2.0-Windows-x86_64.zip", "ppanel-server.exe"},
		{"ppanel-server-1.2.0-Linux-x86_64.tar.gz", "ppanel-server"},
	} {
		t.Run(tc.asset, func(t *testing.T) {
			dest := t.TempDir()
			archive := releaseArchive(t, tc.asset, "docs/"+tc.binary, tc.binary)
			binary, err := NewUpdater().extractBinary(archive, dest, tc.asset)
			require.NoError(t, err)
			require.Equal(t, filepath.Join(dest, tc.binary), binary)
			data, err := os.ReadFile(binary)
			require.NoError(t, err)
			require.Equal(t, "release content of "+tc.binary, string(data))
		})
	}
}
