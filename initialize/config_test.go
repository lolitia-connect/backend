package initialize

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
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

func TestWizardKeepsRecoverableConfigurationAcrossSetupFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.yaml")
	const original = "Port: 0\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	state := newSetupState(path)
	failure := errors.New("storage unavailable")
	stage := "connect"
	state.connect = func(orm.Config) (*sql.DB, string, error) {
		if stage == "connect" {
			return nil, "", failure
		}
		return nil, orm.DriverPostgres, nil
	}
	state.tables = func(*sql.DB, string) ([]string, error) {
		if stage == "tables" {
			return nil, failure
		}
		return nil, nil
	}
	state.migrate = func(orm.Mysql) error { return nil }
	state.createAdmin = func(context.Context, string, string, *sql.DB, string) error { return failure }
	for _, stage = range []string{"connect", "tables", "admin"} {
		if got := submitSetup(state); got != http.StatusInternalServerError {
			t.Fatalf("%s failure returned HTTP %d", stage, got)
		}
		select {
		case <-state.status:
			t.Fatalf("wizard signalled completion after %s failure", stage)
		default:
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if stage != "admin" {
			if string(data) != original {
				t.Fatalf("%s failure replaced the original config", stage)
			}
			continue
		}
		var saved config.File
		if err := conf.Load(path, &saved); err != nil {
			t.Fatal(err)
		}
		if saved.Database.Addr != "localhost:5432" || saved.Administrator.Email != "admin@example.com" || saved.JwtAuth.AccessSecret == "" || strings.Contains(string(data), "strong-password") {
			t.Fatal("administrator failure did not leave a resumable configuration")
		}
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

func TestSetupRedisCheckReportsAuthenticationFailure(t *testing.T) {
	cache := miniredis.RunT(t)
	cache.RequireAuth("fixture-password")
	engine := hertzx.New()
	engine.POST("/init/redis/test", HandleRedisTest)
	host, port, err := net.SplitHostPort(cache.Addr())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		password string
		want     bool
	}{
		{"fixture-password", true},
		{"wrong-password", false},
	} {
		request := engine.Hertz().NewContext()
		request.Request.SetRequestURI("/init/redis/test")
		request.Request.Header.SetMethod(http.MethodPost)
		request.Request.Header.Set("Content-Type", "application/json")
		request.Request.SetBodyString(fmt.Sprintf(`{"host":%q,"port":%q,"password":%q}`, host, port, tc.password))
		engine.Hertz().ServeHTTP(t.Context(), request)
		var result struct {
			Status *bool `json:"status"`
		}
		err := json.Unmarshal(request.Response.Body(), &result)
		if request.Response.StatusCode() != http.StatusOK || err != nil || result.Status == nil || *result.Status != tc.want {
			t.Fatalf("Redis check = HTTP %d, body %s, decode error %v, want status %v", request.Response.StatusCode(), request.Response.Body(), err, tc.want)
		}
	}
}

func TestSetupUsesConfiguredPortAndReportsBindFailure(t *testing.T) {
	t.Setenv("GATEWAY_MODE", "false")
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
