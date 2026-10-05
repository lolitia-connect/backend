package node

import (
	"strings"
	"testing"
)

func TestNormalizeProtocolForStorageNormalizesHysteriaAlias(t *testing.T) {
	protocol, err := NormalizeProtocolForStorage(Protocol{
		Type:     "hysteria2",
		Port:     443,
		Enable:   true,
		Security: "tls",
		SNI:      "node.example",
		CertMode: "self",
	})
	if err != nil {
		t.Fatalf("NormalizeProtocolForStorage() error = %v", err)
	}
	if protocol.Type != "hysteria" {
		t.Fatalf("Type = %q, want hysteria", protocol.Type)
	}
}

func TestNormalizeProtocolForStorageRejectsUnsupportedRuntimeProtocol(t *testing.T) {
	for _, protocolType := range []string{"snell", "unknown"} {
		if _, err := NormalizeProtocolForStorage(Protocol{Type: protocolType}); err == nil {
			t.Fatalf("NormalizeProtocolForStorage(%q) expected error", protocolType)
		}
	}
	// socks, http, naive and mieru are rejected upstream but are offered by this
	// panel, so they must keep saving. Only the enabled protocol types are
	// validated, and none of these has an inbound in node-backend/core/inbound.
	for _, protocolType := range []string{"socks", "http", "naive", "mieru"} {
		if _, err := NormalizeProtocolForStorage(Protocol{Type: protocolType, Enable: true, Port: 1080}); err != nil {
			t.Fatalf("NormalizeProtocolForStorage(%q) unexpected error = %v", protocolType, err)
		}
	}
}

func TestNormalizeProtocolForStorageClearsNoopFrontendValues(t *testing.T) {
	protocol, err := NormalizeProtocolForStorage(Protocol{
		Type:           "vless",
		Security:       "none",
		CertMode:       "none",
		Flow:           "none",
		Obfs:           "none",
		Multiplex:      "none",
		Encryption:     "none",
		EncryptionMode: "native",
		EncryptionRtt:  "0rtt",
	})
	if err != nil {
		t.Fatalf("NormalizeProtocolForStorage() error = %v", err)
	}
	if protocol.Security != "" || protocol.CertMode != "" || protocol.Flow != "" ||
		protocol.Obfs != "" || protocol.Multiplex != "" || protocol.Encryption != "" ||
		protocol.EncryptionMode != "" || protocol.EncryptionRtt != "" {
		t.Fatalf("noop fields were not cleared: %#v", protocol)
	}
}

func TestNormalizeProtocolForStorageRejectsEnabledIncompleteTLS(t *testing.T) {
	_, err := NormalizeProtocolForStorage(Protocol{
		Type:     "hysteria",
		Port:     443,
		Enable:   true,
		Security: "tls",
	})
	if err == nil {
		t.Fatal("NormalizeProtocolForStorage() expected incomplete TLS error")
	}
}

func TestNormalizeProtocolForStorageAllowsDisabledIncompleteTLS(t *testing.T) {
	if _, err := NormalizeProtocolForStorage(Protocol{Type: "hysteria", Security: "tls"}); err != nil {
		t.Fatalf("NormalizeProtocolForStorage() disabled protocol error = %v", err)
	}
}

func TestNormalizeProtocolForStorageAllowsTrojanReality(t *testing.T) {
	protocol, err := NormalizeProtocolForStorage(Protocol{
		Type:              "trojan",
		Port:              443,
		Enable:            true,
		Security:          "reality",
		SNI:               "node.example",
		RealityPrivateKey: "key",
		RealityPublicKey:  "public",
		RealityShortId:    "0123abcd",
	})
	if err != nil {
		t.Fatalf("NormalizeProtocolForStorage() error = %v", err)
	}
	if protocol.Security != "reality" || protocol.RealityPrivateKey != "key" ||
		protocol.RealityPublicKey != "public" || protocol.RealityShortId != "0123abcd" {
		t.Fatalf("reality fields were not preserved: %#v", protocol)
	}
	if protocol.CertMode != "" {
		t.Fatalf("CertMode = %q, want empty for reality", protocol.CertMode)
	}
}

func TestNormalizeProtocolForStorageRejectsTrojanWithoutSecurity(t *testing.T) {
	if _, err := NormalizeProtocolForStorage(Protocol{
		Type:   "trojan",
		Port:   443,
		Enable: true,
	}); err == nil {
		t.Fatal("NormalizeProtocolForStorage() expected trojan security error")
	}
}

func TestNormalizeProtocolForStorageRejectsVlessVisionWithoutSecurity(t *testing.T) {
	if _, err := NormalizeProtocolForStorage(Protocol{
		Type:   "vless",
		Port:   443,
		Enable: true,
		Flow:   "xtls-rprx-vision",
	}); err == nil {
		t.Fatal("NormalizeProtocolForStorage() expected vless vision security error")
	}
}

// The node's shadowsocks inbound implements simple-obfs from these three fields
// (node-backend/core/inbound/shadowsocks.go), so upstream's rule that rejects
// them is deliberately not ported.
func TestNormalizeProtocolForStorageKeepsShadowsocksObfs(t *testing.T) {
	protocol, err := NormalizeProtocolForStorage(Protocol{
		Type:     "shadowsocks",
		Port:     8388,
		Enable:   true,
		Cipher:   "aes-256-gcm",
		Obfs:     "http",
		ObfsHost: "cdn.example",
		ObfsPath: "/obfs",
	})
	if err != nil {
		t.Fatalf("NormalizeProtocolForStorage() error = %v", err)
	}
	if protocol.Obfs != "http" || protocol.ObfsHost != "cdn.example" || protocol.ObfsPath != "/obfs" {
		t.Fatalf("shadowsocks obfs fields were not preserved: %#v", protocol)
	}
}

func TestSanitizeProtocolsForNodeDistributionDropsDisabledAndInvalid(t *testing.T) {
	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{
		{Type: "vless", Id: "a", Port: 443, Enable: false, Security: "none", Transport: "tcp"},
		{Type: "vless", Id: "b", Port: 0, Enable: true, Security: "none", Transport: "tcp"},
		{Type: "shadowsocksr", Id: "c", Port: 443, Enable: true, Cipher: "aes-256-cfb"},
		{Type: "none", Id: "d", Port: 443, Enable: true, Security: "tls"},
	})
	if len(protocols) != 0 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 0 (%#v)", len(protocols), protocols)
	}
}

func TestSanitizeProtocolsForNodeDistributionKeepsEveryInstanceOfAType(t *testing.T) {
	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{
		{Type: "shadowsocks", Id: "a", Port: 8388, Enable: true, Cipher: "aes-128-gcm"},
		{Type: "shadowsocks", Id: "b", Port: 8389, Enable: true, Cipher: "aes-256-gcm"},
		{Type: "shadowsocks", Id: "c", Port: 8390, Enable: false, Cipher: "aes-256-gcm"},
	})
	if len(protocols) != 2 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 2", len(protocols))
	}
	if protocols[0].Id != "a" || protocols[1].Id != "b" {
		t.Fatalf("instance ids were not preserved: %#v", protocols)
	}
}

func TestSanitizeProtocolsForNodeDistributionCleansVlessNoneSecurity(t *testing.T) {
	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{{
		Type:           "vless",
		Port:           443,
		Enable:         true,
		Security:       "none",
		SNI:            "unused.example",
		CertMode:       "none",
		AllowInsecure:  true,
		Fingerprint:    "chrome",
		Encryption:     "none",
		EncryptionMode: "native",
		EncryptionRtt:  "0rtt",
	}})
	if len(protocols) != 1 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 1", len(protocols))
	}
	protocol := protocols[0]
	if protocol.Security != "" || protocol.SNI != "" || protocol.CertMode != "" ||
		protocol.AllowInsecure || protocol.Fingerprint != "" || protocol.EncryptionMode != "" ||
		protocol.EncryptionRtt != "" {
		t.Fatalf("vless runtime fields were not sanitized: %#v", protocol)
	}
}

func TestSanitizeProtocolsForNodeDistributionFiltersIncompleteTLS(t *testing.T) {
	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{{
		Type:     "hysteria2",
		Port:     443,
		Enable:   true,
		Security: "tls",
	}})
	if len(protocols) != 0 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 0", len(protocols))
	}
}

func TestSanitizeProtocolsForNodeDistributionClearsSubscriptionECH(t *testing.T) {
	input := Protocol{
		Type: "vless", Port: 443, Enable: true, Security: "tls",
		SNI: "node.example", CertMode: "http", Transport: "tcp",
		EchEnable: true, EchServerName: "ech.example",
	}
	stored, err := NormalizeProtocolForStorage(input)
	if err != nil {
		t.Fatalf("NormalizeProtocolForStorage() error = %v", err)
	}
	if !stored.EchEnable || stored.EchServerName != "ech.example" {
		t.Fatalf("storage normalization discarded subscription ECH fields: %#v", stored)
	}

	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{input})
	if len(protocols) != 1 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 1", len(protocols))
	}
	if protocols[0].EchEnable || protocols[0].EchServerName != "" {
		t.Fatalf("node distribution leaked subscription ECH fields: %#v", protocols[0])
	}
}

func TestSanitizeProtocolsForNodeDistributionCleansHysteria2Alias(t *testing.T) {
	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{{
		Type:                 "hysteria2",
		Port:                 443,
		Enable:               true,
		Security:             "tls",
		SNI:                  "node.example",
		CertMode:             "self",
		Fingerprint:          "chrome",
		HopPorts:             "20000-30000",
		CongestionController: "bbr",
	}})
	if len(protocols) != 1 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 1", len(protocols))
	}
	protocol := protocols[0]
	if protocol.Type != "hysteria" || protocol.Fingerprint != "" || protocol.HopPorts != "" ||
		protocol.CongestionController != "" {
		t.Fatalf("hysteria runtime fields were not sanitized: %#v", protocol)
	}
}

func TestSanitizeProtocolsForNodeDistributionKeepsShadowsocksObfs(t *testing.T) {
	protocol, err := NormalizeProtocolForStorage(Protocol{
		Type:      "shadowsocks",
		Port:      8388,
		Enable:    true,
		Cipher:    "chacha20-ietf-poly1305",
		Obfs:      "http",
		ObfsHost:  "obfs.example",
		ObfsPath:  "/obfs",
		Multiplex: "smux",
	})
	if err != nil {
		t.Fatalf("NormalizeProtocolForStorage() error = %v", err)
	}
	if protocol.Obfs != "http" || protocol.ObfsHost != "obfs.example" || protocol.ObfsPath != "/obfs" {
		t.Fatalf("shadowsocks obfs fields were cleared: %#v", protocol)
	}
}

func TestSanitizeProtocolsForNodeDistributionKeepsMieruRuntimeFields(t *testing.T) {
	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{{
		Type:      "mieru",
		Port:      23456,
		Enable:    true,
		Transport: "tcp",
		Multiplex: "smux",
	}})
	if len(protocols) != 1 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 1", len(protocols))
	}
	if protocols[0].Transport != "tcp" || protocols[0].Multiplex != "smux" {
		t.Fatalf("mieru fields were sanitized away: %#v", protocols[0])
	}
}

func TestSanitizeProtocolsForNodeDistributionKeepsUnsupportedRuntimeTypes(t *testing.T) {
	// socks, http and naive have no inbound in node-backend, but the panel offers
	// them, so they must not vanish from the node's protocol list.
	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{
		{Type: "socks", Id: "a", Port: 1080, Enable: true, Fingerprint: "chrome"},
		{Type: "http", Id: "b", Port: 8080, Enable: true},
		{Type: "naive", Id: "c", Port: 443, Enable: true, Security: "none", Transport: "tcp"},
	})
	if len(protocols) != 3 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 3", len(protocols))
	}
	if protocols[0].Fingerprint != "" {
		t.Fatalf("socks client-only fields were not cleared: %#v", protocols[0])
	}
	if protocols[2].Transport != "" {
		t.Fatalf("naive transport was not cleared: %#v", protocols[2])
	}
}

func TestSanitizeProtocolsForNodeDistributionClearsMultiplexWhereUnsupported(t *testing.T) {
	cases := []struct {
		name     string
		protocol Protocol
	}{
		{"hysteria", Protocol{
			Type: "hysteria", Port: 443, Enable: true, Security: "tls",
			SNI: "node.example", CertMode: "self", Multiplex: "smux",
		}},
		{"naive", Protocol{
			Type: "naive", Port: 443, Enable: true, Security: "tls",
			SNI: "node.example", CertMode: "self", Multiplex: "smux",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			protocols := SanitizeProtocolsForNodeDistribution([]Protocol{tc.protocol})
			if len(protocols) != 1 {
				t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 1", len(protocols))
			}
			if protocols[0].Multiplex != "" {
				t.Fatalf("node distribution Multiplex = %q, want empty", protocols[0].Multiplex)
			}
		})
	}
}

func TestSanitizeProtocolsForNodeDistributionKeepsAnytlsPaddingScheme(t *testing.T) {
	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{{
		Type: "anytls", Port: 443, Enable: true, Security: "tls",
		SNI: "node.example", CertMode: "self", PaddingScheme: "stop=8\n0=30-30",
	}})
	if len(protocols) != 1 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 1", len(protocols))
	}
	if protocols[0].PaddingScheme != "stop=8\n0=30-30" {
		t.Fatalf("node distribution PaddingScheme = %q, want preserved", protocols[0].PaddingScheme)
	}
}

func TestSanitizeProtocolsForNodeDistributionKeepsTuicCongestionControls(t *testing.T) {
	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{{
		Type: "tuic", Port: 443, Enable: true, Security: "tls",
		SNI: "node.example", CertMode: "self",
		CongestionController: "bbr", ReduceRtt: true, UDPRelayMode: "native",
	}})
	if len(protocols) != 1 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 1", len(protocols))
	}
	protocol := protocols[0]
	if protocol.CongestionController != "bbr" || !protocol.ReduceRtt {
		t.Fatalf("tuic runtime fields were sanitized away: %#v", protocol)
	}
	if protocol.UDPRelayMode != "" {
		t.Fatalf("UDPRelayMode = %q, want empty", protocol.UDPRelayMode)
	}
}

func TestSanitizeProtocolsForNodeDistributionKeepsVlessEncryption(t *testing.T) {
	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{{
		Type: "vless", Port: 443, Enable: true, Security: "tls",
		SNI: "node.example", CertMode: "self", Transport: "ws",
		Encryption: "mlkem768x25519plus", EncryptionMode: "native",
		EncryptionPrivateKey: "private-key",
	}})
	if len(protocols) != 1 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 1", len(protocols))
	}
	protocol := protocols[0]
	if protocol.EncryptionMode != "native" || protocol.EncryptionPrivateKey != "private-key" ||
		protocol.Transport != "ws" {
		t.Fatalf("vless runtime fields were sanitized away: %#v", protocol)
	}
}

func TestSanitizeProtocolsForNodeDistributionClearsVmessFlowAndEncryption(t *testing.T) {
	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{{
		Type: "vmess", Port: 443, Enable: true, Security: "tls",
		SNI: "node.example", CertMode: "self", Transport: "grpc",
		Flow: "xtls-rprx-vision", Encryption: "mlkem768x25519plus",
		EncryptionMode: "native", EncryptionPrivateKey: "private-key",
	}})
	if len(protocols) != 1 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 1", len(protocols))
	}
	protocol := protocols[0]
	if protocol.Flow != "" || protocol.EncryptionMode != "" || protocol.EncryptionPrivateKey != "" {
		t.Fatalf("vmess flow/encryption were not cleared: %#v", protocol)
	}
	if protocol.Transport != "grpc" {
		t.Fatalf("Transport = %q, want grpc", protocol.Transport)
	}
}

func TestSanitizeProtocolsForNodeDistributionKeepsTrojanTransport(t *testing.T) {
	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{{
		Type: "trojan", Port: 443, Enable: true, Security: "tls",
		SNI: "node.example", CertMode: "http", Transport: "ws",
		Host: "cdn.example", Path: "/trojan", ServiceName: "svc",
		XhttpMode: "auto", XhttpExtra: `{"x":1}`,
	}})
	if len(protocols) != 1 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 1", len(protocols))
	}
	protocol := protocols[0]
	if protocol.Transport != "ws" || protocol.Host != "cdn.example" || protocol.Path != "/trojan" ||
		protocol.ServiceName != "svc" || protocol.XhttpMode != "auto" || protocol.XhttpExtra != `{"x":1}` {
		t.Fatalf("trojan stream transport was sanitized away: %#v", protocol)
	}
}

func TestSanitizeProtocolsForNodeDistributionFiltersAnytlsWithoutSecurity(t *testing.T) {
	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{{
		Type: "anytls", Port: 443, Enable: true, SNI: "node.example",
	}})
	if len(protocols) != 0 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 0", len(protocols))
	}
}

func TestNormalizeProtocolForStorageNormalizesNowhereDefaults(t *testing.T) {
	protocol, err := NormalizeProtocolForStorage(Protocol{
		Type: "nowhere", Port: 443, Enable: true, Security: "tls", SNI: "node.example", CertMode: "self",
	})
	if err != nil {
		t.Fatalf("NormalizeProtocolForStorage() error = %v", err)
	}
	if protocol.Version != 1 || protocol.Network != "mix" ||
		protocol.Security != "tls" || protocol.SNI != "node.example" || protocol.CertMode != "self" ||
		len(protocol.ALPN) != 1 || protocol.ALPN[0] != "now/1" {
		t.Fatalf("Nowhere defaults were not normalized: %#v", protocol)
	}
}

func TestNormalizeProtocolForStorageNormalizesNowhereNetwork(t *testing.T) {
	tests := map[string]string{
		"": "mix", "mix": "mix", "mixed": "mix", "both": "mix",
		"tcp,udp": "mix", "udp,tcp": "mix", "tcp+udp": "mix", "udp+tcp": "mix",
		"tcp": "tcp", "udp": "udp", "quic": "udp",
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			protocol, err := NormalizeProtocolForStorage(Protocol{
				Type: "nowhere", Port: 443, Enable: true, Security: "tls", Network: input,
				SNI: "node.example", CertMode: "self",
			})
			if err != nil {
				t.Fatalf("NormalizeProtocolForStorage() error = %v", err)
			}
			if protocol.Network != want {
				t.Fatalf("Network = %q, want %q", protocol.Network, want)
			}
		})
	}
}

func TestNormalizeProtocolForStorageRejectsInvalidNowhere(t *testing.T) {
	base := Protocol{
		Type: "nowhere", Port: 443, Version: 1, Enable: true, Security: "tls",
		Network: "mix", SNI: "node.example", ALPN: []string{"now/1"}, CertMode: "self",
	}
	tests := map[string]func(*Protocol){
		"zero port":          func(p *Protocol) { p.Port = 0 },
		"version":            func(p *Protocol) { p.Version = 2 },
		"security":           func(p *Protocol) { p.Security = "reality" },
		"missing sni":        func(p *Protocol) { p.SNI = " " },
		"missing cert mode":  func(p *Protocol) { p.CertMode = "none" },
		"network":            func(p *Protocol) { p.Network = "ws" },
		"multiple alpn":      func(p *Protocol) { p.ALPN = []string{"now/1", "h2"} },
		"empty alpn":         func(p *Protocol) { p.ALPN = []string{" "} },
		"oversized alpn":     func(p *Protocol) { p.ALPN = []string{strings.Repeat("a", 256)} },
		"transport":          func(p *Protocol) { p.Transport = "tcp" },
		"uot":                func(p *Protocol) { p.UoT = true },
		"multiplex":          func(p *Protocol) { p.Multiplex = "low" },
		"quic option":        func(p *Protocol) { p.ReduceRtt = true },
		"allow insecure":     func(p *Protocol) { p.AllowInsecure = true },
		"reality public key": func(p *Protocol) { p.RealityPublicKey = "key" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			protocol := base
			protocol.ALPN = append([]string(nil), base.ALPN...)
			mutate(&protocol)
			if _, err := NormalizeProtocolForStorage(protocol); err == nil {
				t.Fatal("NormalizeProtocolForStorage() expected error")
			}
		})
	}
}

func TestSanitizeProtocolsForNodeDistributionKeepsNowhere(t *testing.T) {
	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{{
		Type: "nowhere", Port: 443, Enable: true, Security: "tls", Network: "mixed",
		SNI: "node.example", CertMode: "self", CertPinSHA256: strings.ToUpper(testCertPin), Ratio: 1.5,
	}})
	if len(protocols) != 1 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 1", len(protocols))
	}
	protocol := protocols[0]
	if protocol.Type != "nowhere" || protocol.Version != 1 || protocol.Network != "mix" ||
		protocol.Security != "tls" || protocol.SNI != "node.example" || protocol.CertMode != "self" ||
		protocol.CertPinSHA256 != testCertPin || protocol.Ratio != 1.5 ||
		len(protocol.ALPN) != 1 || protocol.ALPN[0] != "now/1" {
		t.Fatalf("Nowhere node distribution fields were not preserved: %#v", protocol)
	}
}

func TestSanitizeProtocolsForNodeDistributionFiltersInvalidNowhere(t *testing.T) {
	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{{
		Type: "nowhere", Port: 443, Enable: true, Security: "tls",
		SNI: "node.example", CertMode: "self", Multiplex: "low",
	}})
	if len(protocols) != 0 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 0", len(protocols))
	}
}

func TestNormalizeProtocolForStorageNormalizesShadowsocksr(t *testing.T) {
	protocol, err := NormalizeProtocolForStorage(Protocol{
		Type: "ssr", Port: 8388, Enable: true,
		Cipher: "AES-256-CFB", ServerKey: "secret",
		SSRProtocol: "AUTH_CHAIN_A", Network: "both",
	})
	if err != nil {
		t.Fatalf("NormalizeProtocolForStorage() error = %v", err)
	}
	if protocol.Type != "shadowsocksr" || protocol.Cipher != "aes-256-cfb" ||
		protocol.SSRProtocol != "auth_chain_a" || protocol.Obfs != "plain" {
		t.Fatalf("ShadowsocksR fields were not normalized: %#v", protocol)
	}
	// Network folds into Transport, which is the field the node reads.
	if protocol.Transport != "both" || protocol.Network != "" {
		t.Fatalf("Transport = %q, Network = %q, want both and empty", protocol.Transport, protocol.Network)
	}
}

func TestNormalizeProtocolForStorageRejectsInvalidShadowsocksr(t *testing.T) {
	base := Protocol{
		Type: "shadowsocksr", Port: 8388, Enable: true,
		Cipher: "aes-256-cfb", ServerKey: "secret", SSRProtocol: "auth_chain_a",
	}
	tests := map[string]func(*Protocol){
		"stream cipher":   func(p *Protocol) { p.Cipher = "aes-256-gcm" },
		"missing key":     func(p *Protocol) { p.ServerKey = "" },
		"legacy protocol": func(p *Protocol) { p.SSRProtocol = "origin" },
		"verify protocol": func(p *Protocol) { p.SSRProtocol = "verify_sha1" },
		"unknown obfs":    func(p *Protocol) { p.Obfs = "tls1.3_ticket_auth" },
		"bad network":     func(p *Protocol) { p.Transport = "ws" },
		// Obfs wraps TCP only, so a UDP listener has nothing to obfuscate.
		"udp with obfs": func(p *Protocol) { p.Transport = "udp"; p.Obfs = "http_simple" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			protocol := base
			mutate(&protocol)
			if _, err := NormalizeProtocolForStorage(protocol); err == nil {
				t.Fatal("NormalizeProtocolForStorage() expected error")
			}
		})
	}
	if _, err := NormalizeProtocolForStorage(Protocol{
		Type: "shadowsocksr", Port: 8388, Enable: true,
		Cipher: "chacha20-ietf", ServerKey: "secret", SSRProtocol: "auth_chain_f",
		Transport: "udp", Obfs: "plain",
	}); err != nil {
		t.Fatalf("udp with plain obfs must be accepted, got %v", err)
	}
}

func TestSanitizeProtocolsForNodeDistributionKeepsShadowsocksr(t *testing.T) {
	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{{
		Type: "shadowsocksr", Port: 8388, Enable: true,
		Cipher: "aes-256-cfb", ServerKey: "secret", SSRProtocol: "auth_chain_a",
		Obfs: "http_simple", ObfsParam: "cdn.example", ProtocolParam: "1:abc",
		Security: "tls", SNI: "node.example", CertMode: "self",
	}})
	if len(protocols) != 1 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 1", len(protocols))
	}
	protocol := protocols[0]
	if protocol.ObfsParam != "cdn.example" || protocol.ProtocolParam != "1:abc" ||
		protocol.SSRProtocol != "auth_chain_a" || protocol.ServerKey != "secret" {
		t.Fatalf("SSR runtime fields were sanitized away: %#v", protocol)
	}
	// SSR is a plaintext protocol: TLS fields must not survive.
	if protocol.Security != "" || protocol.SNI != "" || protocol.CertMode != "" {
		t.Fatalf("SSR TLS fields were not cleared: %#v", protocol)
	}
}

func TestSanitizeProtocolsForNodeDistributionFiltersIncompleteShadowsocksr(t *testing.T) {
	protocols := SanitizeProtocolsForNodeDistribution([]Protocol{{
		Type: "shadowsocksr", Port: 8388, Enable: true,
		Cipher: "aes-256-cfb", SSRProtocol: "auth_chain_a",
	}})
	if len(protocols) != 0 {
		t.Fatalf("SanitizeProtocolsForNodeDistribution() len = %d, want 0", len(protocols))
	}
}

func TestNormalizeProtocolForStorageKeepsCertPinForSelfSignedTLS(t *testing.T) {
	protocol, err := NormalizeProtocolForStorage(Protocol{
		Type:          "vmess",
		Port:          443,
		Enable:        true,
		Security:      "tls",
		SNI:           "node.example",
		CertMode:      "self",
		CertPinSHA256: strings.ToUpper(testCertPin),
	})
	if err != nil {
		t.Fatalf("NormalizeProtocolForStorage() error = %v", err)
	}
	if protocol.CertPinSHA256 != testCertPin {
		t.Fatalf("CertPinSHA256 = %q, want lowercased %q", protocol.CertPinSHA256, testCertPin)
	}
}

func TestNormalizeProtocolForStorageClearsCertPinWhenNotSelfSigned(t *testing.T) {
	cases := []struct {
		name     string
		protocol Protocol
	}{
		{"cert_mode dns", Protocol{
			Type: "vmess", Port: 443, Enable: true, Security: "tls",
			SNI: "node.example", CertMode: "dns", CertDNSProvider: "cloudflare",
			CertPinSHA256: testCertPin,
		}},
		{"security reality", Protocol{
			Type: "vless", Port: 443, Enable: true, Security: "reality",
			SNI: "node.example", RealityPrivateKey: "key", RealityShortId: "0123abcd",
			CertPinSHA256: testCertPin,
		}},
		{"invalid fingerprint", Protocol{
			Type: "vmess", Port: 443, Enable: true, Security: "tls",
			SNI: "node.example", CertMode: "self",
			CertPinSHA256: "not-a-fingerprint",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			protocol, err := NormalizeProtocolForStorage(tc.protocol)
			if err != nil {
				t.Fatalf("NormalizeProtocolForStorage() error = %v", err)
			}
			if protocol.CertPinSHA256 != "" {
				t.Fatalf("CertPinSHA256 = %q, want empty", protocol.CertPinSHA256)
			}
		})
	}
}
