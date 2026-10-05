package adapter

import (
	"testing"

	"github.com/perfect-panel/server/internal/model/node"
)

func TestAdapterProxy(t *testing.T) {
	servers := getServers()
	if len(servers) == 0 {
		t.Fatal("no servers found")
	}

	proxies, err := NewAdapter(tpl).Proxies(servers)
	if err != nil {
		t.Fatalf("failed to adapt servers: %v", err)
	}
	if len(proxies) != 2 {
		t.Fatalf("proxies len = %d, want 2", len(proxies))
	}
	if proxies[0].Name != "TestShadowSocks" || proxies[0].Type != "shadowsocks" {
		t.Fatalf("first proxy = %#v, want shadowsocks proxy", proxies[0])
	}
	if proxies[0].Method != "aes-256-gcm" {
		t.Fatalf("first proxy method = %q, want aes-256-gcm", proxies[0].Method)
	}
	if proxies[1].Name != "TestTrojan" || proxies[1].SNI != "tls.example.com" {
		t.Fatalf("second proxy = %#v, want trojan proxy with SNI", proxies[1])
	}
}

func getServers() []*node.Node {
	srv := &node.Server{
		Id:      1,
		Name:    "TestServer",
		Address: "example.com",
	}
	if err := srv.MarshalProtocols([]node.Protocol{
		{
			Type:   "shadowsocks",
			Port:   1234,
			Enable: true,
			Cipher: "aes-256-gcm",
		},
		{
			Type:      "trojan",
			Port:      443,
			Enable:    true,
			Security:  "tls",
			SNI:       "tls.example.com",
			Transport: "tcp",
		},
	}); err != nil {
		panic(err)
	}

	enabled := true
	return []*node.Node{
		{
			Id:         1,
			Name:       "TestShadowSocks",
			Tags:       "stable,asia",
			Port:       1234,
			Address:    "ss.example.com",
			ServerId:   srv.Id,
			Server:     srv,
			Protocol:   "shadowsocks",
			ProtocolId: "1",
			Enabled:    &enabled,
			Sort:       1,
		},
		{
			Id:         2,
			Name:       "TestTrojan",
			Tags:       "tls",
			Port:       443,
			Address:    "trojan.example.com",
			ServerId:   srv.Id,
			Server:     srv,
			Protocol:   "trojan",
			ProtocolId: "1",
			Enabled:    &enabled,
			Sort:       2,
		},
	}
}

func TestAdapterProxyCertPinForcesCertVerification(t *testing.T) {
	const pin = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	srv := &node.Server{Id: 1, Name: "PinServer", Address: "example.com"}
	if err := srv.MarshalProtocols([]node.Protocol{
		{
			Type: "vmess", Id: "1", Port: 443, Enable: true, Security: "tls",
			SNI: "pin.example.com", CertMode: "self",
			AllowInsecure: true, CertPinSHA256: pin,
		},
		{
			Type: "trojan", Id: "1", Port: 444, Enable: true, Security: "tls",
			SNI: "pin.example.com", CertMode: "http",
			AllowInsecure: true,
		},
	}); err != nil {
		t.Fatalf("marshal protocols: %v", err)
	}

	enabled := true
	nodes := []*node.Node{
		{Id: 1, Name: "Pinned", Port: 443, Address: "a.example.com", ServerId: srv.Id, Server: srv, Protocol: "vmess", ProtocolId: "1", Enabled: &enabled},
		{Id: 2, Name: "Unpinned", Port: 444, Address: "b.example.com", ServerId: srv.Id, Server: srv, Protocol: "trojan", ProtocolId: "1", Enabled: &enabled},
	}
	proxies, err := NewAdapter(tpl).Proxies(nodes)
	if err != nil {
		t.Fatalf("proxies: %v", err)
	}
	if len(proxies) != 2 {
		t.Fatalf("len(proxies) = %d, want 2", len(proxies))
	}
	pinned, unpinned := proxies[0], proxies[1]
	if pinned.CertPinSHA256 != pin {
		t.Fatalf("pinned CertPinSHA256 = %q, want %q", pinned.CertPinSHA256, pin)
	}
	if pinned.AllowInsecure {
		t.Fatal("pinned proxy AllowInsecure = true, want false (pin must force cert verification)")
	}
	if !unpinned.AllowInsecure {
		t.Fatal("unpinned proxy AllowInsecure = false, want true (no pin, keep configured value)")
	}
}
