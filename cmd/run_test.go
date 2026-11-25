package cmd

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/pkg/conf"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func TestPanelRecoversAfterInstallingItsTLSCertificate(t *testing.T) {
	dsn := os.Getenv("PPANEL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PPANEL_TEST_POSTGRES_DSN to run the panel startup integration test")
	}
	database, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	name := fmt.Sprintf("ppanel_startup_%d", time.Now().UnixNano())
	_, err = database.ExecContext(t.Context(), "CREATE DATABASE "+name)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_, err := database.ExecContext(ctx, "DROP DATABASE "+name)
		require.NoError(t, err)
	})
	isolated, err := url.Parse(dsn)
	require.NoError(t, err)
	isolated.Path = "/" + name
	t.Setenv("PPANEL_DB", isolated.String())
	t.Setenv("PPANEL_REDIS", "redis://"+miniredis.RunT(t).Addr())
	t.Setenv("GATEWAY_MODE", "false")
	t.Chdir(t.TempDir())
	previous, writer, hertzLogger, logger := startConfigPath, log.Writer(), hlog.DefaultLogger(), zap.L()
	t.Cleanup(func() {
		startConfigPath = previous
		log.SetOutput(writer)
		hlog.SetLogger(hertzLogger)
		zap.ReplaceGlobals(logger)
	})
	certServer := httptest.NewTLSServer(http.NotFoundHandler())
	cert := certServer.TLS.Certificates[0]
	port := certServer.Listener.Addr().(*net.TCPAddr).Port
	certServer.Close()
	startConfigPath = "panel.yaml"
	initial := fmt.Sprintf("Host: 127.0.0.1\nPort: %d\nTLS: {Enable: true, CertFile: panel.crt, KeyFile: panel.key}\nGeoIP: {Download: false}\nLogger: {Mode: console, Level: error}\nAdministrator: {Email: startup@example.test, Password: startup-test-password}\n", port)
	require.NoError(t, os.WriteFile(startConfigPath, []byte(initial), 0600))
	require.ErrorContains(t, startCmd.RunE(startCmd, nil), "load TLS certificate")
	var installed config.Config
	require.NoError(t, conf.Load(startConfigPath, &installed))
	require.NotEmpty(t, installed.JwtAuth.AccessSecret)
	require.Equal(t, name, installed.DatabaseConfig().Dbname)

	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile("panel.crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600))
	require.NoError(t, os.WriteFile("panel.key", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600))
	group, resources := getServers()
	t.Cleanup(func() { require.NoError(t, resources.Close()) })
	done := make(chan error, 1)
	go func() { done <- group.Run() }()
	t.Cleanup(func() {
		group.Stop()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(15 * time.Second):
			t.Error("panel services did not stop")
		}
	})
	require.Eventually(t, func() bool { return healthcheck(t.Context(), startConfigPath, time.Second) == nil }, 30*time.Second, 100*time.Millisecond)
	identity := resources.Ent.UserAuthMethod.Query().OnlyX(t.Context())
	require.True(t, identity.Verified)
	require.Equal(t, "startup@example.test", identity.AuthIdentifier)
	require.True(t, resources.Ent.User.Query().OnlyX(t.Context()).IsAdmin)
	require.Equal(t, installed.JwtAuth.AccessSecret, resources.Config.JwtAuth.AccessSecret)
}

func TestEnvironmentInstallAtCustomPathPreservesSettings(t *testing.T) {
	previousPath := startConfigPath
	t.Cleanup(func() { startConfigPath = previousPath })
	startConfigPath = filepath.Join(t.TempDir(), "custom.yaml")
	initial := "Host: 127.0.0.2\nPort: 9090\nTLS:\n  Enable: true\n  CertFile: cert.pem\nEdgeSubscribe:\n  Enabled: true\nUnknownSetting: retained\n"
	if err := os.WriteFile(startConfigPath, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PPANEL_DB", "mysql://user:pass@localhost:3306/panel")
	t.Setenv("PPANEL_REDIS", "redis://:redis-pass@localhost:6380/2")
	var settings config.Config
	if initConfig(&settings) {
		t.Fatal("environment installation launched setup for a custom path")
	}
	var persisted config.Config
	if err := conf.Load(startConfigPath, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.DatabaseConfig().Addr == "" || persisted.JwtAuth.AccessSecret == "" || persisted.Redis.Host != "localhost:6380" || persisted.Redis.DB != 2 || persisted.Redis.Pass != "redis-pass" {
		t.Fatal("environment installation did not persist connection settings")
	}
	if !persisted.TLS.Enable || persisted.TLS.CertFile != "cert.pem" || !persisted.EdgeSubscribe.Enabled || persisted.Host != "127.0.0.2" || persisted.Port != 9090 {
		t.Fatal("environment installation dropped existing configuration")
	}
	content, err := os.ReadFile(startConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := yaml.Unmarshal(content, &values); err != nil {
		t.Fatal(err)
	}
	if values["UnknownSetting"] != "retained" {
		t.Fatal("unknown configuration was dropped")
	}
	secret := persisted.JwtAuth.AccessSecret
	if initConfig(&persisted) || persisted.JwtAuth.AccessSecret != secret {
		t.Fatal("existing installation regenerated its signing secret")
	}
}
