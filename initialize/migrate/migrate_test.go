package migrate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

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
