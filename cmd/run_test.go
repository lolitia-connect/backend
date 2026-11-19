package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/pkg/conf"
	"gopkg.in/yaml.v3"
)

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
