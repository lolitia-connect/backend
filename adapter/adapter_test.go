package adapter

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/model/node"
	"gopkg.in/yaml.v3"
)

func TestAdapter_Client(t *testing.T) {
	servers := getServers()
	if len(servers) == 0 {
		t.Errorf("[Test] No servers found")
		return
	}
	a := NewAdapter(tpl, WithServers(servers), WithUserInfo(User{
		Password:     "test-password",
		ExpiredAt:    time.Now().AddDate(1, 0, 0),
		Download:     0,
		Upload:       0,
		Traffic:      1000,
		SubscribeURL: "https://example.com/subscribe",
	}))
	client, err := a.Client()
	if err != nil {
		t.Errorf("[Test] Failed to get client: %v", err.Error())
		return
	}
	bytes, err := client.Build()
	if err != nil {
		t.Errorf("[Test] Failed to build client config: %v", err.Error())
		return
	}
	t.Logf("[Test] Client config built successfully: %s", string(bytes))

}

func TestAdapterClientDuplicateNamesAreLocalToConfig(t *testing.T) {
	servers := getServers()
	servers[0].Name, servers[0].Sort = "香港", 20
	servers[1].Name, servers[1].Sort = "香港", 10
	invalid := *servers[0]
	invalid.ProtocolId, invalid.Sort = "missing", 1

	const configTemplate = `proxies:
{{range .Proxies}}  - name: {{.Name | quote}}
    server: {{.Server | quote}}
{{end}}proxy-groups:
  - name: Auto
    proxies:
{{range .Proxies}}      - {{.Name | quote}}
{{end}}`

	tests := []struct {
		name    string
		servers []*node.Node
		want    []string
	}{
		{"combined", servers, []string{"香港 #2", "香港 #1"}},
		{"first separately", servers[:1], []string{"香港"}},
		{"second with invalid duplicate", []*node.Node{&invalid, servers[1]}, []string{"香港"}},
		{"combined again", servers, []string{"香港 #2", "香港 #1"}},
	}
	for _, format := range []string{"yaml", "base64"} {
		for _, tt := range tests {
			t.Run(format+"/"+tt.name, func(t *testing.T) {
				client, err := NewAdapter(configTemplate, WithServers(tt.servers), WithOutputFormat(format)).Client()
				if err != nil {
					t.Fatal(err)
				}
				output, err := client.Build()
				if err != nil {
					t.Fatal(err)
				}
				if format == "base64" {
					output, err = base64.StdEncoding.DecodeString(string(output))
					if err != nil {
						t.Fatal(err)
					}
				}
				var config struct {
					Proxies []struct {
						Name string `yaml:"name"`
					} `yaml:"proxies"`
					Groups []struct {
						Proxies []string `yaml:"proxies"`
					} `yaml:"proxy-groups"`
				}
				if err := yaml.Unmarshal(output, &config); err != nil {
					t.Fatal(err)
				}
				var names []string
				for _, proxy := range config.Proxies {
					names = append(names, proxy.Name)
				}
				if !reflect.DeepEqual(names, tt.want) {
					t.Fatalf("proxy names = %v, want %v", names, tt.want)
				}
				if len(config.Groups) != 1 || !reflect.DeepEqual(config.Groups[0].Proxies, names) {
					t.Fatalf("group references = %v, want %v", config.Groups, names)
				}
				for _, server := range tt.servers {
					if server.Name != "香港" {
						t.Fatalf("rendering changed source node name to %q", server.Name)
					}
				}
			})
		}
	}
}

func TestClientTemplateCannotReadServerSecrets(t *testing.T) {
	client := &Client{
		ClientTemplate: "{{ .Proxies | toJson }}",
		Proxies: []Proxy{{
			RealityServerAddr: "private-target", RealityServerPort: 9443,
			RealityPrivateKey: "private-key", RealityPublicKey: "public-key",
			EncryptionPrivateKey: "encryption-private", CertDNSEnv: "dns-secret",
			CertDNSProvider: "dns-provider", CertMode: "dns", CertPinSHA256: "public-pin",
		}},
	}
	output, err := client.Build()
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"RealityServerAddr", "RealityServerPort", "RealityPrivateKey", "EncryptionPrivateKey", "CertDNSEnv", "CertDNSProvider", "CertMode"} {
		if strings.Contains(string(output), forbidden) {
			t.Fatalf("template exposes %s", forbidden)
		}
	}
	if !strings.Contains(string(output), "public-key") || !strings.Contains(string(output), "public-pin") {
		t.Fatal("client connection parameters were removed")
	}
}

func TestClientTemplateRejectsHostFunctions(t *testing.T) {
	for _, function := range []string{"env", "expandenv", "getHostByName"} {
		client := &Client{ClientTemplate: "{{ " + function + " \"PATH\" }}"}
		if _, err := client.Build(); err == nil {
			t.Fatalf("template accepted %s", function)
		}
	}
}
