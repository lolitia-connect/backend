package user

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	entwithdrawal "github.com/perfect-panel/server/ent/userwithdrawal"
)

// The withdrawal rows live in the `withdrawals` table because migration 02121
// creates that name. No ent auto-migration runs at startup (only the embedded
// golang-migrate chain does), so a schema annotation that disagreed with the
// migration would send every withdrawal write to a table that does not exist.
func TestWithdrawalTableMatchesMigration(t *testing.T) {
	if entwithdrawal.Table != "withdrawals" {
		t.Fatalf("withdrawal table = %q, want %q", entwithdrawal.Table, "withdrawals")
	}
}

var (
	createOrAlterTable = regexp.MustCompile("(?i)(?:CREATE TABLE(?: IF NOT EXISTS)?|ALTER TABLE)\\s+[`\"]?([A-Za-z_][A-Za-z0-9_]*)[`\"]?")
	tableAnnotation    = regexp.MustCompile(`entsql\.Annotation\{\s*Table:\s*"([^"]+)"`)
)

// Every ent schema's physical table has to be created by one of the embedded
// migrations. This is the general form of the bug above: the annotation and the
// migration chain are two independent declarations of the same fact, and only
// the migration chain actually decides whether the table exists at runtime.
func TestEverySchemaTableHasAMigration(t *testing.T) {
	root := filepath.Join("..", "..", "..")

	created := map[string]bool{}
	for _, dir := range []string{
		filepath.Join(root, "initialize", "migrate", "database", "mysql"),
		filepath.Join(root, "initialize", "migrate", "database", "postgres"),
	} {
		files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no migrations found under %s: %v", dir, err)
		}
		for _, file := range files {
			body, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			for _, match := range createOrAlterTable.FindAllStringSubmatch(string(body), -1) {
				created[match[1]] = true
			}
		}
	}

	schemas, err := filepath.Glob(filepath.Join(root, "ent", "schema", "*.go"))
	if err != nil || len(schemas) == 0 {
		t.Fatalf("no ent schemas found: %v", err)
	}
	checked := 0
	for _, schema := range schemas {
		body, err := os.ReadFile(schema)
		if err != nil {
			t.Fatalf("read %s: %v", schema, err)
		}
		match := tableAnnotation.FindStringSubmatch(string(body))
		if match == nil {
			t.Errorf("%s declares no explicit table annotation", filepath.Base(schema))
			continue
		}
		checked++
		if !created[match[1]] {
			t.Errorf("%s declares table %q, but no embedded migration creates it",
				filepath.Base(schema), match[1])
		}
	}
	if checked == 0 {
		t.Fatal("no annotated schemas were checked")
	}
}
