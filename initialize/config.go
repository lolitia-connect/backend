package initialize

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/perfect-panel/server/initialize/migrate"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/report"
	"github.com/perfect-panel/server/pkg/conf"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/orm"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

//go:embed templates/*.html
var templateFS embed.FS

type setupState struct {
	path        string
	status      chan bool
	mu          sync.Mutex
	completed   bool
	connect     func(orm.Config) (*sql.DB, string, error)
	tables      func(*sql.DB, string) ([]string, error)
	migrate     func(orm.Mysql) error
	createAdmin func(context.Context, string, string, *sql.DB, string) error
}

func newSetupState(path string) *setupState {
	return &setupState{
		path:   path,
		status: make(chan bool, 1),
		connect: func(settings orm.Config) (*sql.DB, string, error) {
			return orm.OpenSQL(orm.Mysql{Config: settings})
		},
		tables: listDatabaseTables,
		migrate: func(database orm.Mysql) error {
			migration := migrate.Migrate(database.Driver(), database.MigrationDsn())
			defer migration.Close()
			err := migration.Up()
			if errors.Is(err, migrate.NoChange) {
				return nil
			}
			return err
		},
		createAdmin: func(ctx context.Context, email, password string, database *sql.DB, driver string) error {
			return migrate.CreateAdminUser(ctx, email, password, orm.NewEntClient(database, driver))
		},
	}
}

func Config(path string) (<-chan bool, *http.Server, error) {
	var settings config.File
	if err := conf.Load(path, &settings); err != nil {
		return nil, nil, err
	}
	state := newSetupState(path)
	// Create a new Hertz-compatible instance.
	r := hertzx.Default()
	// get server port
	port := settings.Port
	host := "127.0.0.1"

	// check gateway mode
	if report.IsGatewayMode() {
		// get free port
		freePort, err := report.ModulePort()
		if err != nil {
			return nil, nil, err
		}
		port = freePort
		// register module
		err = report.RegisterModule(port)
		if err != nil {
			return nil, nil, err
		}
		zap.S().Infof("module registered on port %d", port)
	}
	// Create a new HTTP server
	server := &http.Server{
		Addr:    net.JoinHostPort(host, fmt.Sprint(port)),
		Handler: r,
	}
	// Load templates
	tmpl := template.Must(template.ParseFS(templateFS, "templates/*.html"))
	r.SetHTMLTemplate(tmpl)

	r.GET("/init", handleInit)
	r.POST("/init/config", state.handleInitConfig)
	r.POST("/init/database/test", HandleDatabaseTest)
	r.POST("/init/mysql/test", HandleMySQLTest)
	r.POST("/init/redis/test", HandleRedisTest)
	// Handle 404
	r.NoRoute(func(c *hertzx.Context) {
		c.Redirect(http.StatusFound, "/init")
	})

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return nil, nil, fmt.Errorf("setup listener: %w", err)
	}
	server.Addr = listener.Addr().String()
	zap.S().Infof("setup wizard at http://%s/init; use an SSH tunnel for remote access", server.Addr)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			zap.S().Errorf("setup listener failed: %v", err)
			select {
			case state.status <- false:
			default:
			}
		}
	}()

	return state.status, server, nil
}

func handleInit(c *hertzx.Context) {
	c.HTML(http.StatusOK, "index.html", nil)
}
func (state *setupState) handleInitConfig(c *hertzx.Context) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.completed {
		c.JSON(http.StatusConflict, hertzx.H{"code": 409, "msg": "Installation already completed"})
		return
	}
	// Load configuration file

	var cfg config.File
	if err := conf.Load(state.path, &cfg); err != nil {
		c.JSON(http.StatusInternalServerError, hertzx.H{"code": 500, "msg": "Configuration initialization failed"})
		return
	}
	var request struct {
		AdminEmail    string `json:"adminEmail"`
		AdminPassword string `json:"adminPassword"`

		DatabaseDriver string `json:"databaseDriver"`
		MysqlHost      string `json:"mysqlHost"`
		MysqlPort      string `json:"mysqlPort"`
		MysqlDatabase  string `json:"mysqlDatabase"`
		MysqlUser      string `json:"mysqlUser"`
		MysqlPassword  string `json:"mysqlPassword"`

		RedisHost     string `json:"redisHost"`
		RedisPort     string `json:"redisPort"`
		RedisPassword string `json:"redisPassword"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, hertzx.H{
			"code": 400,
			"msg":  "Invalid request",
			"data": nil,
		})
		c.Abort()
		return
	}
	request.AdminEmail = strings.ToLower(strings.TrimSpace(request.AdminEmail))
	if err := validateSetupAdministrator(request.AdminEmail, request.AdminPassword); err != nil {
		c.JSON(http.StatusBadRequest, hertzx.H{"code": 400, "msg": err.Error()})
		return
	}
	cfg.Debug = false
	// jwt secret
	cfg.JwtAuth.AccessSecret = uuid.New().String()
	// database
	dbConfig, err := buildDatabaseConfig(request.DatabaseDriver, request.MysqlHost, request.MysqlPort, request.MysqlDatabase, request.MysqlUser, request.MysqlPassword)
	if err != nil {
		c.JSON(http.StatusBadRequest, hertzx.H{
			"code": 400,
			"msg":  err.Error(),
			"data": nil,
		})
		c.Abort()
		return
	}
	cfg.SetDatabaseConfig(dbConfig)
	// redis
	cfg.Redis.Host = fmt.Sprintf("%s:%s", request.RedisHost, request.RedisPort)
	cfg.Redis.Pass = request.RedisPassword
	cfg.Administrator.Email = request.AdminEmail
	cfg.Administrator.Password = ""

	// save config
	fileData, err := yaml.Marshal(cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, hertzx.H{
			"code": 500,
			"msg":  "Configuration initialization failed",
			"data": nil,
		})
		c.Abort()
		return
	}

	// create database connection
	dbClient := orm.Mysql{Config: dbConfig}
	db, driver, err := state.connect(dbConfig)
	if err != nil {
		c.JSON(http.StatusInternalServerError, hertzx.H{
			"code": 500,
			"msg":  "Database connection failed",
			"data": nil,
		})
		c.Abort()
		return
	}
	if db != nil {
		defer db.Close()
	}
	tables, err := state.tables(db, driver)
	if err != nil {
		c.JSON(http.StatusInternalServerError, hertzx.H{"code": 500, "msg": "Database table check failed"})
		return
	}
	if len(tables) != 0 {
		c.JSON(http.StatusBadRequest, hertzx.H{"code": 400, "msg": "The database contains existing data. Please clear it before proceeding with the installation."})
		return
	}
	if err := os.WriteFile(state.path, fileData, 0600); err != nil {
		c.JSON(http.StatusInternalServerError, hertzx.H{"code": 500, "msg": "Configuration initialization failed"})
		return
	}
	if err := os.Chmod(state.path, 0600); err != nil {
		c.JSON(http.StatusInternalServerError, hertzx.H{"code": 500, "msg": "Configuration initialization failed"})
		return
	}
	// migrate database
	if err = state.migrate(dbClient); err != nil {
		zap.S().Errorf("[Init Database] Migrate failed: %v", err.Error())
		c.JSON(http.StatusInternalServerError, hertzx.H{
			"code": 500,
			"msg":  "Database migration failed. Configuration was saved; fix the database and restart to resume installation. A new administrator password will be printed once on successful bootstrap.",
			"data": nil,
		})
		c.Abort()
		return
	}

	// create admin user
	if err = state.createAdmin(c.Request.Context(), request.AdminEmail, request.AdminPassword, db, driver); err != nil {
		zap.S().Errorf("[Init Database] Create admin user failed: %v", err.Error())
		c.JSON(http.StatusInternalServerError, hertzx.H{
			"code": 500,
			"msg":  "Admin user creation failed. Configuration was saved; restart to resume installation. A new administrator password will be printed once on successful bootstrap.",
			"data": nil,
		})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, hertzx.H{
		"code":   200,
		"msg":    "Configuration initialized",
		"status": true,
	})
	state.completed = true
	select {
	case state.status <- true:
	default:
	}
}

func validateSetupAdministrator(email, password string) error {
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return fmt.Errorf("a valid administrator email is required")
	}
	if utf8.RuneCountInString(password) < 8 {
		return fmt.Errorf("administrator password must contain at least eight characters")
	}
	return nil
}

func HandleMySQLTest(c *hertzx.Context) {
	HandleDatabaseTest(c)
}

func HandleDatabaseTest(c *hertzx.Context) {
	var request struct {
		Driver   string `json:"driver"`
		Host     string `json:"host"`
		Port     string `json:"port"`
		Database string `json:"database"`
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, hertzx.H{
			"code": 400,
			"msg":  "Invalid request",
			"data": nil,
		})
		c.Abort()
		return
	}
	var status = true
	var message string
	var tx *sql.DB
	var tables []string
	dbConfig, err := buildDatabaseConfig(request.Driver, request.Host, request.Port, request.Database, request.User, request.Password)
	if err != nil {
		c.JSON(http.StatusOK, hertzx.H{
			"code":   200,
			"msg":    err.Error(),
			"status": false,
		})
		return
	}
	db, driverName, err := orm.OpenSQL(orm.Mysql{Config: dbConfig})
	if err != nil {
		zap.S().Errorf("connect database failed, err: %v\n", err.Error())
		status = false
		message = "Database connection failed"
		goto result
	}
	tx = db
	if tx != nil {
		defer tx.Close()
	}
	if err := tx.PingContext(c.Request.Context()); err != nil {
		zap.S().Errorf("ping database failed, err: %v\n", err.Error())
		status = false
		message = "Database connection failed"
		goto result
	}

	tables, err = listDatabaseTables(tx, driverName)
	if err != nil {
		zap.S().Errorf("database table check failed, err: %v\n", err.Error())
		status = false
		message = "Database table check failed"
		goto result
	}
	if len(tables) > 0 {
		status = false
		message = "The database contains existing data. Please clear it before proceeding with the installation."
		goto result
	}

result:
	c.JSON(http.StatusOK, hertzx.H{
		"code":   200,
		"msg":    message,
		"status": status,
	})
}

func buildDatabaseConfig(driver, host, port, database, user, password string) (orm.Config, error) {
	normalizedDriver := orm.NormalizeDriver(driver)
	switch normalizedDriver {
	case orm.DriverMySQL, orm.DriverPostgres:
	default:
		return orm.Config{}, fmt.Errorf("unsupported database driver: %s", driver)
	}
	cfg := orm.Config{
		Driver:        normalizedDriver,
		Addr:          fmt.Sprintf("%s:%s", host, port),
		Username:      user,
		Password:      password,
		Dbname:        database,
		MaxIdleConns:  10,
		MaxOpenConns:  10,
		SlowThreshold: orm.DefaultSlowThresholdMs,
	}
	if normalizedDriver == orm.DriverPostgres {
		cfg.Config = orm.DefaultPostgresConfig
	} else {
		cfg.Config = orm.DefaultMySQLConfig
	}
	return cfg, nil
}

func listDatabaseTables(db *sql.DB, driver string) ([]string, error) {
	query := "SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()"
	if orm.NormalizeDriver(driver) == orm.DriverPostgres {
		query = "SELECT table_name FROM information_schema.tables WHERE table_schema = 'public'"
	}
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}
	return tables, rows.Err()
}

func HandleRedisTest(c *hertzx.Context) {
	var request struct {
		Host     string `json:"host"`
		Port     string `json:"port"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, hertzx.H{
			"code": 400,
			"msg":  "Invalid request",
			"data": nil,
		})
		c.Abort()
		return
	}
	if err := tool.RedisPing(fmt.Sprintf("%s:%s", request.Host, request.Port), request.Password, 0); err != nil {
		c.JSON(http.StatusOK, hertzx.H{
			"code":   200,
			"msg":    nil,
			"status": false,
		})
		return
	}
	c.JSON(http.StatusOK, hertzx.H{
		"code":   200,
		"msg":    nil,
		"status": true,
	})
}
