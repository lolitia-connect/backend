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
	"github.com/stretchr/testify/require"
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

func TestTrafficIndexMigrationCanResumeAfterAnIncompleteSchema(t *testing.T) {
	dsn := os.Getenv("PPANEL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PPANEL_TEST_POSTGRES_DSN to run the traffic index migration test")
	}
	ctx := t.Context()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	exec := func(query string) {
		t.Helper()
		_, err := tx.ExecContext(ctx, query)
		require.NoError(t, err)
	}
	exec(`SET LOCAL search_path TO pg_temp`)
	exec(`CREATE TEMP TABLE traffic_log (server_id bigint, user_id bigint, timestamp timestamptz, download bigint) ON COMMIT DROP`)
	exec(`INSERT INTO traffic_log VALUES (10, 20, CURRENT_TIMESTAMP, 350)`)
	exec(`CREATE INDEX idx_server_id ON traffic_log(server_id)`)
	exec(`CREATE INDEX idx_user_id ON traffic_log(user_id)`)
	exec(`CREATE INDEX idx_timestamp ON traffic_log(timestamp)`)
	indexes := func() []string {
		t.Helper()
		rows, err := tx.QueryContext(ctx, `SELECT indexname FROM pg_indexes WHERE schemaname = (SELECT nspname FROM pg_namespace WHERE oid = pg_my_temp_schema()) AND tablename = 'traffic_log' ORDER BY indexname`)
		require.NoError(t, err)
		defer rows.Close()
		var names []string
		for rows.Next() {
			var name string
			require.NoError(t, rows.Scan(&name))
			names = append(names, name)
		}
		require.NoError(t, rows.Err())
		return names
	}
	const migration = "database/postgres/02155_traffic_log_hot_indexes."
	original := indexes()
	exec(`SAVEPOINT before_upgrade`)
	require.ErrorContains(t, ExecuteSQLFile(tx, migration+"up.sql"), "subscribe_id")
	exec(`ROLLBACK TO SAVEPOINT before_upgrade`)
	require.Equal(t, original, indexes())
	exec(`ALTER TABLE traffic_log ADD COLUMN subscribe_id bigint DEFAULT 30`)
	require.NoError(t, ExecuteSQLFile(tx, migration+"up.sql"))
	require.ElementsMatch(t, []string{"idx_traffic_server_time", "idx_traffic_user_sub_time", "idx_traffic_log_timestamp_brin"}, indexes())
	require.NoError(t, ExecuteSQLFile(tx, migration+"down.sql"))
	require.Equal(t, original, indexes())
	var download int64
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT download FROM traffic_log WHERE user_id = 20 AND subscribe_id = 30`).Scan(&download))
	require.EqualValues(t, 350, download)
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
