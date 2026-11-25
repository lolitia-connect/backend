package updater

import (
	"archive/zip"
	"bytes"
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
	_, err = u.Download(&Release{Assets: []Asset{{Name: u.getAssetName(), BrowserDownloadURL: "https://release.example/binary"}}})
	require.ErrorContains(t, err, "status code 502")
	u.HTTPClient.Transport = failingUpdateTransport{}
	_, err = u.Download(&Release{Assets: []Asset{{Name: u.getAssetName(), BrowserDownloadURL: "https://release.example/binary"}}})
	require.ErrorContains(t, err, "failed to download asset")
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
