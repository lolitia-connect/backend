package logging

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDailyLogRotationCompressesPreviousFileAndRemovesOldBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, accessFilename)
	previous := "previous day's request\n"
	if err := os.WriteFile(path, []byte(previous), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := path + "-2000-01-01"
	if err := os.WriteFile(stale, []byte("old requests\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -30)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	writer, err := newRotateWriter(path, normalize(LogConf{Compress: true, KeepDays: 7}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	writer.currentDay = time.Now().AddDate(0, 0, -1).Format(time.DateOnly)
	if _, err := writer.Write([]byte("first request today\nsecond request today\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Sync(); err != nil {
		t.Fatal(err)
	}
	lines, err := ReadLastNLines(dir, 1)
	if err != nil || !slices.Equal(lines, []string{"second request today"}) {
		t.Fatalf("latest requests = %q, %v", lines, err)
	}
	backups := writer.backups()
	if len(backups) != 1 || !strings.HasSuffix(backups[0], ".gz") {
		t.Fatalf("backups after retention = %v", backups)
	}
	archive, err := os.Open(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	decoded, err := gzip.NewReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer decoded.Close()
	content, err := io.ReadAll(decoded)
	if err != nil || string(content) != previous {
		t.Fatalf("compressed log = %q, %v", content, err)
	}
}

func TestReadLastNLinesAcrossReadBoundary(t *testing.T) {
	dir := t.TempDir()
	last := strings.Repeat("z", 5000)
	if err := os.WriteFile(filepath.Join(dir, accessFilename), []byte("discarded\r\nprior request\r\n"+last), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := ReadLastNLines(dir, 2)
	if err != nil || !slices.Equal(lines, []string{"prior request", last}) {
		t.Fatalf("trailing requests not preserved across a read boundary: %d lines, %v", len(lines), err)
	}
}
