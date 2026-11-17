package migrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/perfect-panel/server/pkg/orm"
)

func TestMigrateMySQL(t *testing.T) {
	dsn := os.Getenv("PPANEL_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set PPANEL_TEST_MYSQL_DSN to run MySQL/MariaDB migration test")
	}
	runMigration(t, orm.DriverMySQL, dsn)
}

func TestMigratePostgres(t *testing.T) {
	dsn := os.Getenv("PPANEL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PPANEL_TEST_POSTGRES_DSN to run PostgreSQL migration test")
	}
	runMigration(t, orm.DriverPostgres, dsn)
}

func TestMySQLProtocolInstanceBackfill(t *testing.T) {
	dsn := os.Getenv("PPANEL_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set PPANEL_TEST_MYSQL_DSN to run MySQL/MariaDB migration test")
	}
	config, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.MultiStatements = true
	databaseName := fmt.Sprintf("ppanel_protocol_test_%d", time.Now().UnixNano())
	admin, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+databaseName+"`"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := admin.ExecContext(cleanup, "DROP DATABASE `"+databaseName+"`"); err != nil {
			t.Error(err)
		}
	}()
	config.DBName = databaseName
	database, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.ExecContext(ctx, "CREATE TABLE nodes (id BIGINT PRIMARY KEY, protocol VARCHAR(100)); CREATE TABLE servers (id BIGINT PRIMARY KEY, protocols LONGTEXT); INSERT INTO nodes VALUES (1, 'vless')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO servers VALUES (1, ?)", `[{"type":"vless","port":443},{"type":"trojan","id":"custom","port":444},{"type":"vless","port":445},{"type":"trojan","port":446}]`); err != nil {
		t.Fatal(err)
	}
	migration, err := sqlFiles.ReadFile("database/mysql/02149_protocol_instance_id.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := database.ExecContext(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
		var raw string
		if err := database.QueryRowContext(ctx, "SELECT protocols FROM servers WHERE id = 1").Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var protocols []struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Port int    `json:"port"`
		}
		if err := json.Unmarshal([]byte(raw), &protocols); err != nil {
			t.Fatal(err)
		}
		if len(protocols) != 4 {
			t.Fatalf("protocols changed: %s", raw)
		}
		for index, expected := range []string{"1", "custom", "2", "2"} {
			if protocols[index].ID != expected || protocols[index].Port != 443+index {
				t.Fatalf("protocol instance changed: %s", raw)
			}
		}
		var instance string
		if err := database.QueryRowContext(ctx, "SELECT protocol_id FROM nodes WHERE id = 1").Scan(&instance); err != nil || instance != "1" {
			t.Fatalf("node instance = %q, %v", instance, err)
		}
	}
}

func runMigration(t *testing.T, driver, dsn string) {
	t.Helper()
	err := Migrate(driver, dsn).Up()
	if err != nil && !errors.Is(err, NoChange) {
		t.Fatalf("%s migration failed: %v", driver, err)
	}
	cfg := orm.ParseDSN(dsn)
	if cfg == nil {
		t.Fatalf("%s dsn parse failed", driver)
	}
	cfg.Driver = orm.NormalizeDriver(driver)
	db, driverName, err := orm.OpenSQL(orm.Mysql{Config: *cfg})
	if err != nil {
		t.Fatalf("%s connect failed: %v", driver, err)
	}
	defer db.Close()
	if err := CreateAdminUser(context.Background(), fmt.Sprintf("admin-%s@example.com", driver), "password", orm.NewEntClient(db, driverName)); err != nil {
		t.Fatalf("%s create admin failed: %v", driver, err)
	}
}
