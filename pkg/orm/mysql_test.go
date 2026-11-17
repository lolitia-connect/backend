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
	for _, test := range []struct {
		query string
		key   string
		zone  string
	}{
		{"sslmode=disable", "TimeZone", "Asia/Shanghai"},
		{"sslmode=require&TimeZone=UTC", "TimeZone", "UTC"},
		{"timezone=Europe%2FBerlin", "timezone", "Europe/Berlin"},
		{"time_zone=America%2FNew_York&application_name=panel%2Bworker", "time_zone", "America/New_York"},
	} {
		database := Mysql{Config: Config{Driver: DriverPostgres, Addr: "localhost:5432", Dbname: "panel", Config: test.query}}
		parsed, err := url.Parse(database.Dsn())
		if err != nil {
			t.Fatal(err)
		}
		if got := parsed.Query().Get(test.key); got != test.zone {
			t.Fatalf("query %q has zone %q, want %q", test.query, got, test.zone)
		}
		if strings.Contains(test.query, "application_name") && parsed.Query().Get("application_name") != "panel+worker" {
			t.Fatal("PostgreSQL parameters were decoded twice")
		}
	}
}
