package adapter

import (
	"strings"
	"testing"
	"time"
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
