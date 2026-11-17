package initialize

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/pkg/conf"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/orm"
)

func submitSetup(state *setupState) int {
	engine := hertzx.New()
	engine.POST("/init/config", state.handleInitConfig)
	request := engine.Hertz().NewContext()
	request.Request.SetRequestURI("/init/config")
	request.Request.Header.SetMethod(http.MethodPost)
	request.Request.Header.Set("Content-Type", "application/json")
	request.Request.SetBodyString(`{"adminEmail":"Admin@Example.com","adminPassword":"strong-password","mysqlHost":"localhost","mysqlPort":"5432","mysqlDatabase":"panel","databaseDriver":"postgres"}`)
	engine.Hertz().ServeHTTP(context.Background(), request)
	return request.Response.StatusCode()
}

func TestSetupChecksDatabaseBeforeSavingAndSavesBeforeMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.yaml")
	if err := os.WriteFile(path, []byte("Port: 8080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	state := newSetupState(path)
	state.connect = func(orm.Config) (*sql.DB, string, error) { return nil, orm.DriverPostgres, nil }
	state.tables = func(*sql.DB, string) ([]string, error) { return []string{"users"}, nil }
	if got := submitSetup(state); got != http.StatusBadRequest {
		t.Fatalf("existing database status = %d", got)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "Port: 8080\n" {
		t.Fatalf("configuration changed for existing database: %q, %v", data, err)
	}
	state.tables = func(*sql.DB, string) ([]string, error) { return nil, nil }
	state.migrate = func(orm.Mysql) error {
		var settings config.File
		if err := conf.Load(path, &settings); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || settings.Database.Addr != "localhost:5432" || settings.Administrator.Email != "admin@example.com" || settings.JwtAuth.AccessSecret == "" || strings.Contains(string(data), "strong-password") {
			t.Fatalf("resumable configuration was not saved before migration: %+v, %v", settings, err)
		}
		return errors.New("migration interrupted")
	}
	if got := submitSetup(state); got != http.StatusInternalServerError {
		t.Fatalf("failed migration status = %d", got)
	}
}

func TestConcurrentSetupCreatesAdministratorOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.yaml")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	state := newSetupState(path)
	state.connect = func(orm.Config) (*sql.DB, string, error) { return nil, orm.DriverPostgres, nil }
	state.tables = func(*sql.DB, string) ([]string, error) { return nil, nil }
	state.migrate = func(orm.Mysql) error { return nil }
	created := 0
	state.createAdmin = func(context.Context, string, string, *sql.DB, string) error { created++; return nil }
	var workers sync.WaitGroup
	results := make(chan int, 2)
	for index := 0; index < 2; index++ {
		workers.Add(1)
		go func() { defer workers.Done(); results <- submitSetup(state) }()
	}
	workers.Wait()
	close(results)
	successes := 0
	for status := range results {
		if status == http.StatusOK {
			successes++
		} else if status != http.StatusConflict {
			t.Fatalf("submission status = %d", status)
		}
	}
	if created != 1 || successes != 1 || len(state.status) != 1 {
		t.Fatalf("created=%d successes=%d signals=%d", created, successes, len(state.status))
	}
}

func TestSetupAdministratorValidation(t *testing.T) {
	for _, test := range []struct {
		email    string
		password string
		valid    bool
	}{
		{"admin@example.com", "strong-password", true},
		{"admin@example.com", "short", false},
		{"invalid", "strong-password", false},
		{"Admin <admin@example.com>", "strong-password", false},
	} {
		if err := validateSetupAdministrator(test.email, test.password); (err == nil) != test.valid {
			t.Fatalf("email %q valid=%v, error=%v", test.email, test.valid, err)
		}
	}
}

func TestSetupUsesConfiguredPortAndReportsBindFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	path := filepath.Join(t.TempDir(), "setup.yaml")
	port := listener.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(path, []byte("Port: "+strconv.Itoa(port)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, server, err := Config(path); err == nil {
		_ = server.Shutdown(context.Background())
		t.Fatal("setup accepted an occupied configured port")
	}
	if err := os.WriteFile(path, []byte("Port: 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, server, err := Config(path)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown(context.Background())
	host, _, err := net.SplitHostPort(server.Addr)
	if err != nil || host != "127.0.0.1" {
		t.Fatalf("wizard listener = %q, %v", server.Addr, err)
	}
}
