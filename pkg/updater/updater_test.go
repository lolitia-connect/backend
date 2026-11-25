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

func TestDownloadSelectsPlatformAssetAndExtractsBinary(t *testing.T) {
	const content = "replacement program"
	u := NewUpdater()
	u.CurrentVersion = "v1.1.0"
	name := u.getAssetName()
	var archive bytes.Buffer
	var executable = "ppanel-server"
	if strings.HasSuffix(name, ".zip") {
		executable += ".exe"
		writer := zip.NewWriter(&archive)
		for _, file := range []string{"README", "nested/" + executable, executable} {
			entry, err := writer.Create(file)
			require.NoError(t, err)
			_, err = io.WriteString(entry, content+file)
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())
	} else {
		compressed := gzip.NewWriter(&archive)
		writer := tar.NewWriter(compressed)
		for _, file := range []string{"README", "nested/" + executable, executable} {
			body := content + file
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: file, Mode: 0o755, Size: int64(len(body))}))
			_, err := io.WriteString(writer, body)
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())
		require.NoError(t, compressed.Close())
	}
	const assetURL = "https://releases.example.test/platform"
	var requested []string
	u.HTTPClient.Transport = updateTransport(func(req *http.Request) (*http.Response, error) {
		requested = append(requested, req.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(archive.Bytes()))}, nil
	})
	_, err := u.Download(&Release{Assets: []Asset{{Name: "checksums.txt"}}})
	require.ErrorContains(t, err, "no suitable asset")
	require.Empty(t, requested)
	binary, err := u.Download(&Release{Assets: []Asset{
		{Name: "checksums.txt", BrowserDownloadURL: "https://releases.example.test/checksums"},
		{Name: name, BrowserDownloadURL: assetURL},
	}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(binary)) })
	require.Equal(t, []string{assetURL}, requested)
	require.Equal(t, executable, filepath.Base(binary))
	data, err := os.ReadFile(binary)
	require.NoError(t, err)
	require.Equal(t, content+executable, string(data))
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

func TestGetAssetName(t *testing.T) {
	u := NewUpdater()
	u.CurrentVersion = "v1.0.0"

	assetName := u.getAssetName()
	assert.NotEmpty(t, assetName)
	assert.Contains(t, assetName, "ppanel-server")
	assert.Contains(t, assetName, "v1.0.0")
}
