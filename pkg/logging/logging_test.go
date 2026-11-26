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

func TestSizeRotationRetainsTheLatestCompressedBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, accessFilename)
	other := filepath.Join(dir, "access-worker.log.gz")
	if err := os.WriteFile(other, []byte("worker diagnostics"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := normalize(LogConf{MaxSize: 1, MaxBackup: 2, Compress: true})
	// A coarse timestamp is valid configuration. Several rotations can share it.
	cfg.FileTimeFormat = "2006-01-02"
	writer, err := newRotateWriter(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	for _, marker := range []string{"a", "b", "c", "d", "e", "f"} {
		if _, err := io.WriteString(writer, strings.Repeat(marker, megabyte-1)+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Sync(); err != nil {
		t.Fatal(err)
	}
	untouched, err := os.ReadFile(other)
	if err != nil || string(untouched) != "worker diagnostics" {
		t.Fatalf("rotation changed the adjacent service log: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "access-2*.gz"))
	if err != nil || len(matches) != 2 {
		t.Fatalf("retained %d compressed backups, want 2: %v", len(matches), err)
	}
	var archived []string
	for _, name := range matches {
		file, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		gz, err := gzip.NewReader(file)
		if err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		data, err := io.ReadAll(gz)
		_ = gz.Close()
		_ = file.Close()
		if err != nil || len(data) != megabyte {
			t.Fatalf("incomplete backup of %d bytes: %v", len(data), err)
		}
		archived = append(archived, string(data[:1]))
	}
	slices.Sort(archived)
	if !slices.Equal(archived, []string{"d", "e"}) {
		t.Fatalf("retained records %v, want d and e", archived)
	}
	active, err := os.ReadFile(path)
	if err != nil || string(active) != strings.Repeat("f", megabyte-1)+"\n" {
		t.Fatalf("latest log lost after rotation: %v", err)
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
