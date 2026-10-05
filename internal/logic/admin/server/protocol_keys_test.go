package server

import (
	"testing"

	"github.com/perfect-panel/server/internal/model/node"
)

func TestApplyGeneratedProtocolKeysGeneratesRealityKeysForAnyProtocol(t *testing.T) {
	for _, protocolType := range []string{"vless", "trojan"} {
		t.Run(protocolType, func(t *testing.T) {
			protocol := &node.Protocol{
				Type:     protocolType,
				Security: "reality",
				SNI:      "www.example.com",
			}
			if err := applyGeneratedProtocolKeys(protocol); err != nil {
				t.Fatalf("applyGeneratedProtocolKeys: %v", err)
			}
			if protocol.RealityPrivateKey == "" || protocol.RealityPublicKey == "" || protocol.RealityShortId == "" {
				t.Fatalf("reality keys were not generated: %+v", protocol)
			}
			if protocol.RealityServerAddr != "www.example.com" {
				t.Fatalf("reality server addr = %q, want SNI", protocol.RealityServerAddr)
			}
			if protocol.RealityServerPort != 443 {
				t.Fatalf("reality server port = %d, want 443", protocol.RealityServerPort)
			}
		})
	}
}

func TestApplyGeneratedProtocolKeysKeepsProvidedRealityKeys(t *testing.T) {
	protocol := &node.Protocol{
		Type:              "vless",
		Security:          "reality",
		RealityPrivateKey: "private",
		RealityPublicKey:  "public",
		RealityShortId:    "abcd",
		RealityServerAddr: "custom.example.com",
		RealityServerPort: 8443,
	}
	if err := applyGeneratedProtocolKeys(protocol); err != nil {
		t.Fatalf("applyGeneratedProtocolKeys: %v", err)
	}
	if protocol.RealityPrivateKey != "private" || protocol.RealityPublicKey != "public" || protocol.RealityShortId != "abcd" {
		t.Fatal("provided reality keys must not be overwritten")
	}
	if protocol.RealityServerAddr != "custom.example.com" || protocol.RealityServerPort != 8443 {
		t.Fatal("provided reality endpoint must not be overwritten")
	}
}

func TestApplyGeneratedProtocolKeysIgnoresNonReality(t *testing.T) {
	protocol := &node.Protocol{Type: "trojan", Security: "tls"}
	if err := applyGeneratedProtocolKeys(protocol); err != nil {
		t.Fatalf("applyGeneratedProtocolKeys: %v", err)
	}
	if protocol.RealityPrivateKey != "" || protocol.RealityPublicKey != "" || protocol.RealityShortId != "" {
		t.Fatal("non-reality protocols must not receive reality keys")
	}
}

func TestApplyGeneratedProtocolKeysGeneratesShadowsocks2022Key(t *testing.T) {
	cases := []struct {
		cipher string
		length int
	}{
		{"2022-blake3-aes-128-gcm", 16},
		{"2022-blake3-aes-256-gcm", 32},
		{"2022-blake3-chacha20-poly1305", 32},
	}
	for _, c := range cases {
		t.Run(c.cipher, func(t *testing.T) {
			protocol := &node.Protocol{Type: "shadowsocks", Cipher: c.cipher}
			if err := applyGeneratedProtocolKeys(protocol); err != nil {
				t.Fatalf("applyGeneratedProtocolKeys: %v", err)
			}
			if len(protocol.ServerKey) != c.length {
				t.Fatalf("server key length = %d, want %d", len(protocol.ServerKey), c.length)
			}
		})
	}
}
