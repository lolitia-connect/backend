package migrate

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Every migration the binary embeds has to be reachable and reversible: a
// missing half or a misnamed file only shows up at deploy time otherwise, and
// only when a real database is at hand.
//
// An empty *down* file is a legitimate no-op in golang-migrate (the fork ships a
// few for seed and DDL migrations it cannot reverse faithfully), so it is
// reported rather than failed. An empty *up* file never is: it would record a
// version as applied without doing anything.
func TestEmbeddedMigrationsAreReachable(t *testing.T) {
	for _, dir := range []string{"database/mysql", "database/postgres"} {
		t.Run(dir, func(t *testing.T) {
			src, err := iofs.New(sqlFiles, dir)
			if err != nil {
				t.Fatalf("open %s: %v", dir, err)
			}
			defer src.Close()

			version, err := src.First()
			if err != nil {
				t.Fatalf("%s has no migrations: %v", dir, err)
			}

			seen := 0
			for {
				assertMigrationContent(t, src, dir, version, "up")

				body := readMigration(t, src, dir, version, "down")
				if len(body) == 0 {
					t.Logf("%s: migration %d has an empty down file (no-op rollback)", dir, version)
				}
				seen++

				next, err := src.Next(version)
				if errors.Is(err, os.ErrNotExist) {
					break
				}
				if err != nil {
					t.Fatalf("%s: advance past %d: %v", dir, version, err)
				}
				if next <= version {
					t.Fatalf("%s: version did not advance from %d to %d", dir, version, next)
				}
				version = next
			}

			if seen == 0 {
				t.Fatalf("%s embedded no readable migrations", dir)
			}
		})
	}
}

func assertMigrationContent(t *testing.T, src source.Driver, dir string, version uint, direction string) {
	t.Helper()

	if body := readMigration(t, src, dir, version, direction); len(body) == 0 {
		t.Fatalf("%s: migration %d has an empty %s file", dir, version, direction)
	}
}

func readMigration(t *testing.T, src source.Driver, dir string, version uint, direction string) []byte {
	t.Helper()

	var (
		reader io.ReadCloser
		err    error
	)
	if direction == "up" {
		reader, _, err = src.ReadUp(version)
	} else {
		reader, _, err = src.ReadDown(version)
	}
	if err != nil {
		t.Fatalf("%s: migration %d has no %s file: %v", dir, version, direction, err)
	}
	defer reader.Close()

	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("%s: read %s of migration %d: %v", dir, direction, version, err)
	}
	return body
}
