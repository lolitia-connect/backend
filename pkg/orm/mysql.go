package orm

import (
	"net/url"
	"strings"
	"time"
)

const (
	DriverMySQL     = "mysql"
	DriverPostgres  = "postgres"
	DriverPostgres2 = "postgresql"

	DefaultMySQLConfig     = "charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai"
	DefaultPostgresConfig  = "sslmode=disable&TimeZone=Asia/Shanghai"
	DefaultSlowThresholdMs = 1000
)

type Config struct {
	Driver        string `yaml:"Driver" default:"mysql"`
	Addr          string `yaml:"Addr"`
	Username      string `yaml:"Username"`
	Password      string `yaml:"Password"`
	Dbname        string `yaml:"Dbname"`
	Config        string `yaml:"Config" default:"charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai"`
	MaxIdleConns  int    `yaml:"MaxIdleConns" default:"10"`
	MaxOpenConns  int    `yaml:"MaxOpenConns" default:"10"`
	SlowThreshold int64  `yaml:"SlowThreshold" default:"1000"`
}

type Mysql struct {
	Config Config
}

func NormalizeDriver(driver string) string {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "", DriverMySQL:
		return DriverMySQL
	case DriverPostgres, DriverPostgres2, "pgsql":
		return DriverPostgres
	default:
		return strings.ToLower(strings.TrimSpace(driver))
	}
}

func (m Mysql) Driver() string {
	return NormalizeDriver(m.Config.Driver)
}

func (m Mysql) Dsn() string {
	switch m.Driver() {
	case DriverPostgres:
		return m.postgresDsn()
	default:
		return m.mysqlDsn()
	}
}

func (m Mysql) MigrationDsn() string {
	if m.Driver() != DriverMySQL {
		return m.Dsn()
	}
	return url.QueryEscape(m.Config.Username) + ":" + url.QueryEscape(m.Config.Password) + "@tcp(" + m.Config.Addr + ")/" + m.Config.Dbname + "?" + m.mysqlQuery()
}

func (m Mysql) mysqlDsn() string {
	return m.Config.Username + ":" + m.Config.Password + "@tcp(" + m.Config.Addr + ")/" + m.Config.Dbname + "?" + m.mysqlQuery()
}

func (m Mysql) mysqlQuery() string {
	query := m.Config.Config
	if query == "" {
		query = DefaultMySQLConfig
	}
	return query
}

func (m Mysql) postgresDsn() string {
	query := m.Config.Config
	if query == "" || query == DefaultMySQLConfig {
		query = DefaultPostgresConfig
	}
	u := url.URL{
		Scheme: DriverPostgres,
		Host:   m.Config.Addr,
		Path:   "/" + m.Config.Dbname,
	}
	if m.Config.Username != "" {
		u.User = url.UserPassword(m.Config.Username, m.Config.Password)
	}
	params, err := url.ParseQuery(query)
	if err != nil {
		u.RawQuery = query
		return u.String()
	}
	if params.Get("TimeZone") == "" && params.Get("timezone") == "" && params.Get("time_zone") == "" {
		params.Set("TimeZone", "Asia/Shanghai")
	}
	u.RawQuery = params.Encode()
	return u.String()
}

func (m *Mysql) GetSlowThreshold() time.Duration {
	return time.Duration(m.Config.SlowThreshold) * time.Millisecond
}
func (m *Mysql) GetColorful() bool {
	return true
}
