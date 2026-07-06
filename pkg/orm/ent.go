package orm

import (
	"database/sql"
	"errors"
	"fmt"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
)

func ConnectEnt(m Mysql) (*ent.Client, error) {
	db, driverName, err := OpenSQL(m)
	if err != nil {
		return nil, err
	}
	return NewEntClient(db, driverName), nil
}

func NewEntClient(db *sql.DB, driverName string) *ent.Client {
	drv := entsql.OpenDB(driverName, db)
	return ent.NewClient(ent.Driver(drv))
}

func OpenSQL(m Mysql) (*sql.DB, string, error) {
	if m.Config.Dbname == "" {
		return nil, "", errors.New("database name is empty")
	}
	driver := m.Driver()
	switch driver {
	case DriverMySQL:
		driver = "mysql"
	case DriverPostgres:
		driver = "postgres"
	default:
		return nil, "", fmt.Errorf("unsupported database driver: %s", m.Config.Driver)
	}
	db, err := sql.Open(driver, m.Dsn())
	if err != nil {
		return nil, "", err
	}
	db.SetMaxIdleConns(m.Config.MaxIdleConns)
	db.SetMaxOpenConns(m.Config.MaxOpenConns)
	return db, driver, nil
}
