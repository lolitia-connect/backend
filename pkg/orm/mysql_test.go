package orm

import (
	"net/url"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestMigrationDSNPreservesMySQLCredentials(t *testing.T) {
	config := Config{Addr: "127.0.0.1:3306", Username: "panel+user", Password: "p%+@:/?&word", Dbname: "panel"}
	database := Mysql{Config: config}
	parsed, err := mysql.ParseDSN(database.MigrationDsn())
	if err != nil {
		t.Fatal(err)
	}
	username, userErr := url.QueryUnescape(parsed.User)
	password, passErr := url.QueryUnescape(parsed.Passwd)
	if userErr != nil || passErr != nil || username != config.Username || password != config.Password {
		t.Fatal("migration driver cannot recover original credentials")
	}
	if !strings.HasPrefix(database.Dsn(), config.Username+":"+config.Password+"@") {
		t.Fatal("ordinary MySQL credentials were changed")
	}
}

func TestPostgresDSNAlwaysPinsSessionZone(t *testing.T) {
	for _, query := range []string{
		"sslmode=disable",
		"sslmode=require&TimeZone=UTC",
		"timezone=Europe%2FBerlin",
		"time_zone=America%2FNew_York&application_name=panel%2Bworker",
	} {
		database := Mysql{Config: Config{Driver: DriverPostgres, Addr: "localhost:5432", Dbname: "panel", Config: query}}
		parsed, err := url.Parse(database.Dsn())
		if err != nil {
			t.Fatal(err)
		}
		values := parsed.Query()
		// The database stores UTC, so the session zone is always pinned to UTC
		// and the lower-case aliases are dropped.
		if got := values.Get("TimeZone"); got != "UTC" {
			t.Fatalf("query %q has TimeZone %q, want UTC", query, got)
		}
		if values.Get("timezone") != "" || values.Get("time_zone") != "" {
			t.Fatalf("query %q left a conflicting timezone alias", query)
		}
		if strings.Contains(query, "application_name") && values.Get("application_name") != "panel+worker" {
			t.Fatal("PostgreSQL parameters were decoded twice")
		}
	}
}
