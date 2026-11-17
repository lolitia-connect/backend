package conf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type Server struct {
	Host string `yaml:"Host" default:"localhost"`
	Port int    `yaml:"Port" default:"8080"`
}

type Config struct {
	Server Server `yaml:"Server"`
}

func TestConfigLoad(t *testing.T) {
	var c Config
	MustLoad("./config_test.yaml", &c)
	t.Logf("config: %+v", c)
}

func TestScalarDefaultsAndExplicitZero(t *testing.T) {
	type namedPort uint16
	settings := struct {
		Port    namedPort     `default:"8080"`
		Small   int8          `default:"-12"`
		Ratio   float32       `default:"1.5"`
		Timeout time.Duration `default:"30s"`
		Enabled bool          `yaml:"enabled" default:"true"`
	}{}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("enabled: false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Load(path, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Port != 8080 || settings.Small != -12 || settings.Ratio != 1.5 || settings.Timeout != 30*time.Second || settings.Enabled {
		t.Fatalf("defaults or explicit false lost: %+v", settings)
	}
}

func TestInvalidDefaultsReturnFieldErrors(t *testing.T) {
	for _, settings := range []any{
		&struct {
			Nested struct {
				Value int8 `default:"128"`
			}
		}{},
		&struct {
			Nested struct {
				Value int `default:"12garbage"`
			}
		}{},
		&struct {
			Nested struct {
				Value bool `default:"maybe"`
			}
		}{},
		&struct {
			Nested struct {
				Value []string `default:"unsupported"`
			}
		}{},
	} {
		if err := Load("unused.yaml", settings); err == nil || !strings.Contains(err.Error(), "Nested.Value") {
			t.Fatalf("invalid default did not identify field: %v", err)
		}
	}
}
