package config

import (
	"encoding/json"
	"testing"

	"github.com/perfect-panel/server/pkg/orm"
)

func TestNodeConfigRoundTripPreservesOutboundSettings(t *testing.T) {
	original := NodeConfig{
		NodeSecret:       "secret",
		NodePullInterval: 30,
		DNS:              []NodeDNS{{Proto: "udp", Address: "1.1.1.1", Domains: []string{"example.test"}}},
		Outbound:         []NodeOutbound{{Name: "proxy", Protocol: "vless", Address: "proxy.test", Port: 443, UUID: "id", Rules: []string{"DOMAIN-SUFFIX,example.test,DIRECT"}}},
	}
	encoded, err := original.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var decoded NodeConfig
	if err := decoded.Unmarshal(encoded); err != nil {
		t.Fatal(err)
	}
	if decoded.NodeSecret != original.NodeSecret || decoded.NodePullInterval != original.NodePullInterval || len(decoded.DNS) != 1 || decoded.DNS[0].Address != "1.1.1.1" || len(decoded.Outbound) != 1 || decoded.Outbound[0].Protocol != "vless" || decoded.Outbound[0].Rules[0] != original.Outbound[0].Rules[0] {
		t.Fatalf("round trip changed node configuration: %+v", decoded)
	}
}

func TestNodeConfigRejectsMalformedJSON(t *testing.T) {
	var config NodeConfig
	if err := config.Unmarshal([]byte(`{"port":`)); err == nil {
		t.Fatal("malformed node configuration was accepted")
	}
}

func TestDatabaseConfigPrefersModernAndSupportsLegacyFallback(t *testing.T) {
	legacy := orm.Config{Driver: orm.DriverMySQL, Addr: "legacy:3306", Dbname: "panel"}
	modern := orm.Config{Driver: orm.DriverPostgres, Addr: "modern:5432", Dbname: "panel"}
	config := Config{MySQL: &legacy}
	if got := config.DatabaseConfig(); got.Addr != legacy.Addr {
		t.Fatalf("legacy database config = %+v", got)
	}
	config.Database = modern
	if got := config.DatabaseConfig(); got.Addr != modern.Addr || config.MySQL == nil {
		t.Fatalf("modern database config = %+v", got)
	}
	config.SetDatabaseConfig(legacy)
	if config.MySQL != nil || config.DatabaseConfig().Addr != legacy.Addr {
		t.Fatalf("SetDatabaseConfig left legacy field or wrong config: %+v", config)
	}
	file := File{MySQL: &modern}
	file.SetDatabaseConfig(legacy)
	if file.MySQL != nil || file.DatabaseConfig().Addr != legacy.Addr {
		t.Fatalf("File.SetDatabaseConfig left legacy field or wrong config: %+v", file)
	}
	if _, err := json.Marshal(config); err != nil {
		t.Fatal(err)
	}
}
