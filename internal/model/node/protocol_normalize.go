package node

import (
	"fmt"
	"strings"
)

// NormalizeProtocolForStorage validates a protocol definition and clears the
// fields that carry no meaning for its type. The admin write paths run it, so an
// inbound the node cannot serve is rejected while the administrator is still
// editing instead of being saved and then silently ignored by the runtime.
//
// Ported from upstream's protocol_normalize.go, restricted to the protocol types
// node-backend/core/inbound actually implements. Upstream's shadowsocksr, snell,
// nowhere and mieru branches are omitted because they need Protocol fields that
// do not exist here (SSRProtocol, ProtocolParam, ObfsParam, Version, Mode,
// Network, ALPN, Plugin, PluginOptions, Heartbeat, QUICCongestionControl,
// TrafficPattern, UserHintIsMandatory, CertPinSHA256).
func NormalizeProtocolForStorage(protocol Protocol) (Protocol, error) {
	protocol.Type = normalizeProtocolType(protocol.Type)
	if !supportedRuntimeProtocol(protocol.Type) {
		return Protocol{}, fmt.Errorf("unsupported protocol type: %s", protocol.Type)
	}
	// Common cleanup discards fields that are irrelevant to most protocols.
	// Nowhere's contract is stricter: an enabled config carrying any unrelated
	// option is rejected rather than silently accepted after cleanup.
	if protocol.Type == "nowhere" && protocol.Enable {
		if err := validateNowhereUnsupportedOptions(protocol); err != nil {
			return Protocol{}, err
		}
	}
	normalizeProtocolNoopFields(&protocol)
	if protocol.Enable {
		if err := validateRuntimeProtocol(&protocol); err != nil {
			return Protocol{}, err
		}
	}
	return protocol, nil
}

func normalizeProtocolType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "hysteria", "hysteria2":
		return "hysteria"
	case "ssr", "shadowsocks-r", "shadowsocksr":
		return "shadowsocksr"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

// supportedRuntimeProtocol mirrors the protocol list the panel offers
// (internal/config/protocol.go and the admin server form). Types outside it
// cannot be provisioned by any code path here.
func supportedRuntimeProtocol(protocol string) bool {
	switch protocol {
	case "shadowsocks", "shadowsocksr", "hysteria", "anytls", "trojan", "vless", "vmess", "tuic",
		"socks", "naive", "http", "mieru", "nowhere":
		return true
	default:
		return false
	}
}

func normalizeProtocolNoopFields(protocol *Protocol) {
	protocol.Security = normalizeNone(protocol.Security)
	protocol.CertMode = normalizeNone(protocol.CertMode)
	protocol.Flow = normalizeNone(protocol.Flow)
	protocol.Obfs = normalizeNone(protocol.Obfs)
	protocol.Multiplex = normalizeDisabled(protocol.Multiplex)
	protocol.Encryption = normalizeNone(protocol.Encryption)
	if protocol.Security != "tls" {
		clearCertificate(protocol)
	}
	if protocol.Security != "reality" {
		clearReality(protocol)
	}
	if protocol.CertMode != "dns" {
		protocol.CertDNSProvider = ""
		protocol.CertDNSEnv = ""
	}
	// The pin mirrors the node's self-signed certificate; under any other
	// cert_mode a stored value is stale and would break client pinning.
	protocol.CertPinSHA256 = NormalizeCertPinSHA256(protocol.CertPinSHA256)
	if protocol.CertMode != "self" {
		protocol.CertPinSHA256 = ""
	}
	if protocol.Encryption == "" {
		clearEncryption(protocol)
	}
}

func normalizeNone(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "none" {
		return ""
	}
	return value
}

func normalizeDisabled(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "", "none", "false", "off", "disabled":
		return ""
	default:
		return value
	}
}

// validateRuntimeProtocol covers the protocol types the node implements. Types
// without an inbound here (socks, naive, http, mieru) deliberately fall through
// unvalidated: there is no runtime to validate them against.
func validateRuntimeProtocol(protocol *Protocol) error {
	switch protocol.Type {
	case "nowhere":
		if protocol.Port == 0 {
			return fmt.Errorf("nowhere requires a non-zero port")
		}
		if protocol.Security != "tls" {
			return fmt.Errorf("nowhere requires tls security")
		}
		if protocol.Version == 0 {
			protocol.Version = 1
		}
		if protocol.Version != 1 {
			return fmt.Errorf("nowhere requires version 1")
		}
		network, err := normalizeNowhereNetwork(protocol.Network)
		if err != nil {
			return err
		}
		protocol.Network = network
		if len(protocol.ALPN) == 0 {
			protocol.ALPN = []string{"now/1"}
		}
		if len(protocol.ALPN) != 1 {
			return fmt.Errorf("nowhere requires exactly one alpn value")
		}
		protocol.ALPN[0] = strings.TrimSpace(protocol.ALPN[0])
		if len(protocol.ALPN[0]) == 0 || len(protocol.ALPN[0]) > 255 {
			return fmt.Errorf("nowhere alpn must be between 1 and 255 bytes")
		}
		protocol.SNI = strings.TrimSpace(protocol.SNI)
		if err := validateNowhereUnsupportedOptions(*protocol); err != nil {
			return err
		}
	case "hysteria", "tuic":
		if protocol.Security != "tls" {
			return fmt.Errorf("%s requires tls security", protocol.Type)
		}
	case "anytls", "trojan":
		if protocol.Security != "tls" && protocol.Security != "reality" {
			return fmt.Errorf("%s requires tls or reality security", protocol.Type)
		}
	case "shadowsocksr":
		transport := strings.ToLower(strings.TrimSpace(protocol.Transport))
		if transport == "" {
			transport = strings.ToLower(strings.TrimSpace(protocol.Network))
		}
		switch transport {
		case "", "both", "tcp,udp", "tcp+udp", "tcp", "udp":
		default:
			return fmt.Errorf("shadowsocksr network is invalid")
		}
		protocol.Transport = transport
		protocol.Network = ""
		protocol.Cipher = strings.ToLower(strings.TrimSpace(protocol.Cipher))
		protocol.SSRProtocol = strings.ToLower(strings.TrimSpace(protocol.SSRProtocol))
		protocol.Obfs = strings.ToLower(strings.TrimSpace(protocol.Obfs))
		if !validShadowsocksrCipher(protocol.Cipher) {
			return fmt.Errorf("shadowsocksr cipher is invalid")
		}
		if protocol.ServerKey == "" {
			return fmt.Errorf("shadowsocksr requires server_key")
		}
		if !validShadowsocksrProtocol(protocol.SSRProtocol) {
			return fmt.Errorf("shadowsocksr protocol is invalid")
		}
		if protocol.Obfs == "" {
			protocol.Obfs = "plain"
		}
		if !validShadowsocksrObfs(protocol.Obfs) {
			return fmt.Errorf("shadowsocksr obfs is invalid")
		}
		// Obfs only wraps TCP, so a UDP-only listener has nothing to obfuscate.
		if protocol.Transport == "udp" && protocol.Obfs != "plain" {
			return fmt.Errorf("shadowsocksr udp-only transport requires plain obfs")
		}
	}
	if protocolRequiresTLSCertificate(*protocol) && !hasTLSCertificate(*protocol) {
		return fmt.Errorf("%s requires sni and cert_mode", protocol.Type)
	}
	if protocol.Type == "vless" && protocol.Flow == "xtls-rprx-vision" &&
		protocol.Security != "tls" && protocol.Security != "reality" {
		return fmt.Errorf("vless vision requires tls or reality security")
	}
	return nil
}

// validShadowsocksrCipher lists the stream ciphers the SSR inbound can build.
func validShadowsocksrCipher(cipher string) bool {
	switch strings.ToLower(strings.TrimSpace(cipher)) {
	case "none", "aes-128-ctr", "aes-192-ctr", "aes-256-ctr", "aes-128-cfb", "aes-192-cfb", "aes-256-cfb", "rc4-md5", "chacha20", "chacha20-ietf":
		return true
	default:
		return false
	}
}

// validShadowsocksrProtocol only allows the protocols that carry a wire UID:
// origin and the legacy verify/auth families cap a node at exactly one active
// user, which defeats the multi-user support this port exists for.
func validShadowsocksrProtocol(protocol string) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "auth_aes128_md5", "auth_aes128_sha1", "auth_chain_a", "auth_chain_b",
		"auth_chain_c", "auth_chain_d", "auth_chain_e", "auth_chain_f":
		return true
	default:
		return false
	}
}

func validShadowsocksrObfs(obfs string) bool {
	switch strings.ToLower(strings.TrimSpace(obfs)) {
	case "plain", "http_simple", "http_post", "tls1.0_session_auth",
		"tls1.2_ticket_auth", "tls1.2_ticket_fastauth":
		return true
	default:
		return false
	}
}

// normalizeNowhereNetwork maps the listener network spellings the admin form and
// older rows can carry onto the three values the Nowhere inbound understands.
func normalizeNowhereNetwork(raw string) (string, error) {
	value := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), " ", ""))
	switch value {
	case "", "mix", "mixed", "both", "tcp,udp", "udp,tcp", "tcp+udp", "udp+tcp":
		return "mix", nil
	case "tcp":
		return "tcp", nil
	case "udp", "quic":
		return "udp", nil
	default:
		return "", fmt.Errorf("nowhere network must be mix, tcp, or udp")
	}
}

// validateNowhereUnsupportedOptions rejects the options Nowhere has no concept
// of. Fields the local model does not carry (Plugin, PluginOptions, Heartbeat,
// QUICCongestionControl, TrafficPattern, UserHintIsMandatory) are absent from
// these checks rather than assumed empty.
func validateNowhereUnsupportedOptions(protocol Protocol) error {
	if protocol.Mode != "" || protocol.AllowInsecure || protocol.Fingerprint != "" ||
		protocol.RealityServerAddr != "" || protocol.RealityServerPort != 0 || protocol.RealityPrivateKey != "" ||
		protocol.RealityPublicKey != "" || protocol.RealityShortId != "" || protocol.Transport != "" ||
		protocol.Host != "" || protocol.Path != "" || protocol.ServiceName != "" || protocol.Cipher != "" ||
		protocol.ServerKey != "" || protocol.Flow != "" ||
		protocol.UoT || protocol.UoTVersion != 0 || protocol.AcceptProxyProtocol || protocol.HopPorts != "" ||
		protocol.HopInterval != 0 || protocol.ObfsPassword != "" || protocol.DisableSNI || protocol.ReduceRtt ||
		protocol.UDPRelayMode != "" || protocol.CongestionController != "" ||
		protocol.Multiplex != "" || protocol.PaddingScheme != "" || protocol.UpMbps != 0 || protocol.DownMbps != 0 ||
		protocol.Obfs != "" || protocol.SSRProtocol != "" || protocol.ProtocolParam != "" || protocol.ObfsParam != "" ||
		protocol.ObfsHost != "" || protocol.ObfsPath != "" || protocol.XhttpMode != "" || protocol.XhttpExtra != "" ||
		protocol.Encryption != "" || protocol.EncryptionMode != "" || protocol.EncryptionRtt != "" ||
		protocol.EncryptionTicket != "" || protocol.EncryptionServerPadding != "" ||
		protocol.EncryptionPrivateKey != "" || protocol.EncryptionClientPadding != "" ||
		protocol.EncryptionPassword != "" || protocol.EchEnable || protocol.EchServerName != "" {
		return fmt.Errorf("nowhere contains unsupported protocol options")
	}
	return nil
}

func protocolRequiresTLSCertificate(protocol Protocol) bool {
	if protocol.Security != "tls" {
		return false
	}
	switch protocol.Type {
	case "anytls", "hysteria", "nowhere", "trojan", "tuic", "vless", "vmess":
		return true
	default:
		return false
	}
}

func hasTLSCertificate(protocol Protocol) bool {
	return protocol.SNI != "" && protocol.CertMode != ""
}

func clearCertificate(protocol *Protocol) {
	protocol.CertMode = ""
	protocol.CertDNSProvider = ""
	protocol.CertDNSEnv = ""
	protocol.CertPinSHA256 = ""
}

func clearReality(protocol *Protocol) {
	protocol.RealityServerAddr = ""
	protocol.RealityServerPort = 0
	protocol.RealityPrivateKey = ""
	protocol.RealityPublicKey = ""
	protocol.RealityShortId = ""
}

func clearEncryption(protocol *Protocol) {
	protocol.EncryptionMode = ""
	protocol.EncryptionRtt = ""
	protocol.EncryptionTicket = ""
	protocol.EncryptionServerPadding = ""
	protocol.EncryptionPrivateKey = ""
	protocol.EncryptionClientPadding = ""
	protocol.EncryptionPassword = ""
}

// SanitizeProtocolsForNodeDistribution projects the stored protocol list onto
// what a node is allowed to see. Disabled or unusable inbounds are dropped and
// every client-only field is cleared, so the runtime never receives settings it
// cannot act on (fingerprint, allow_insecure, ECH).
//
// Ported from upstream's protocol_normalize.go. The upstream clear helpers are
// reproduced as-is except where node-backend reads the field:
//   - shadowsocks keeps Obfs/ObfsHost/ObfsPath. Upstream clears them because its
//     obfs lives in a client plugin; node-backend builds the simple-obfs TCP
//     header from them.
//   - mieru keeps Multiplex and Transport, both read by node-backend's mieru
//     inbound.
//   - naive, socks and http are projected but never filtered out. Upstream drops
//     socks/http and forces tls with a certificate on naive; the panel offers all
//     three, no node inbound exists for them, and the admin form allows naive with
//     security none.
//
// shadowsocksr, snell and nowhere are absent, as in NormalizeProtocolForStorage.
func SanitizeProtocolsForNodeDistribution(protocols []Protocol) []Protocol {
	result := make([]Protocol, 0, len(protocols))
	for _, protocol := range protocols {
		protocol, err := NormalizeProtocolForStorage(protocol)
		if err != nil || !protocol.Enable {
			continue
		}
		// ECH config is consumed only by subscription clients. Nodes receive the
		// inbound runtime projection and must not see client-only ECH settings.
		protocol.EchEnable = false
		protocol.EchServerName = ""
		if sanitizeRuntimeProtocol(&protocol) {
			result = append(result, protocol)
		}
	}
	return result
}

func sanitizeRuntimeProtocol(protocol *Protocol) bool {
	switch protocol.Type {
	case "shadowsocks":
		protocol.Security = ""
		protocol.SNI = ""
		clearCertificate(protocol)
		clearTLSClient(protocol)
		clearReality(protocol)
		// clearLegacyObfs is intentionally skipped: the node reads Obfs, ObfsHost
		// and ObfsPath to build its simple-obfs TCP header.
		clearStreamTransport(protocol)
		clearQUICControls(protocol)
		clearEncryption(protocol)
		return protocol.Port > 0 && protocol.Cipher != ""
	case "mieru":
		protocol.Security = ""
		protocol.SNI = ""
		clearTLSClient(protocol)
		clearCertificate(protocol)
		clearReality(protocol)
		// Multiplex and Transport are kept: node-backend reads both for mieru.
		return protocol.Port > 0
	case "shadowsocksr":
		protocol.Security = ""
		protocol.SNI = ""
		protocol.Multiplex = ""
		clearTLSClient(protocol)
		clearCertificate(protocol)
		clearReality(protocol)
		clearQUICControls(protocol)
		clearEncryption(protocol)
		return protocol.Port > 0 && protocol.Cipher != "" && protocol.ServerKey != "" && protocol.SSRProtocol != ""
	case "hysteria":
		protocol.Security = "tls"
		// The node rejects a multiplex value on QUIC inbounds.
		protocol.Multiplex = ""
		clearTLSClient(protocol)
		clearReality(protocol)
		clearStreamTransport(protocol)
		clearQUICControls(protocol)
		clearEncryption(protocol)
		return protocol.Port > 0 && hasTLSCertificate(*protocol)
	case "tuic":
		protocol.Security = "tls"
		protocol.DisableSNI = false
		protocol.UDPRelayMode = ""
		clearTLSClient(protocol)
		clearReality(protocol)
		clearStreamTransport(protocol)
		clearLegacyObfs(protocol)
		clearEncryption(protocol)
		return protocol.Port > 0 && hasTLSCertificate(*protocol)
	case "anytls":
		clearTLSClient(protocol)
		clearStreamTransport(protocol)
		clearLegacyObfs(protocol)
		clearEncryption(protocol)
		protocol.Multiplex = ""
		if protocol.Security == "tls" {
			clearReality(protocol)
			return protocol.Port > 0 && hasTLSCertificate(*protocol)
		}
		if protocol.Security == "reality" {
			clearCertificate(protocol)
			return protocol.Port > 0 && protocol.SNI != "" && protocol.RealityPrivateKey != "" && protocol.RealityShortId != ""
		}
		return false
	case "trojan":
		clearTLSClient(protocol)
		clearLegacyObfs(protocol)
		clearEncryption(protocol)
		if protocol.Security == "tls" {
			clearReality(protocol)
			return protocol.Port > 0 && hasTLSCertificate(*protocol)
		}
		if protocol.Security == "reality" {
			clearCertificate(protocol)
			return protocol.Port > 0 && protocol.SNI != "" && protocol.RealityPrivateKey != "" && protocol.RealityShortId != ""
		}
		clearCertificate(protocol)
		clearReality(protocol)
		return protocol.Port > 0
	case "vless", "vmess":
		clearLegacyObfs(protocol)
		if protocol.Type != "vless" {
			protocol.Flow = ""
			clearEncryption(protocol)
		}
		if protocol.Security == "tls" {
			clearTLSClient(protocol)
			clearReality(protocol)
			return protocol.Port > 0 && hasTLSCertificate(*protocol)
		}
		if protocol.Security == "reality" {
			clearCertificate(protocol)
			return protocol.Port > 0 && protocol.SNI != "" && protocol.RealityPrivateKey != "" && protocol.RealityShortId != ""
		}
		clearTLSClient(protocol)
		protocol.SNI = ""
		clearCertificate(protocol)
		clearReality(protocol)
		return protocol.Port > 0
	case "naive", "socks", "http":
		protocol.Multiplex = ""
		clearTLSClient(protocol)
		clearLegacyObfs(protocol)
		clearStreamTransport(protocol)
		clearEncryption(protocol)
		return protocol.Port > 0
	case "nowhere":
		protocol.Security = "tls"
		clearTLSClient(protocol)
		clearReality(protocol)
		clearNowhereUnsupported(protocol)
		return protocol.Port > 0 && protocol.Version == 1 && hasTLSCertificate(*protocol) && len(protocol.ALPN) == 1
	default:
		return false
	}
}

// clearNowhereUnsupported drops the fields the Nowhere inbound has no concept
// of. The model has no Plugin/PluginOptions/Heartbeat/QUICCongestionControl/
// TrafficPattern/UserHintIsMandatory fields, so those clears are absent.
func clearNowhereUnsupported(protocol *Protocol) {
	protocol.Mode = ""
	protocol.Cipher = ""
	protocol.ServerKey = ""
	protocol.Flow = ""
	protocol.UoT = false
	protocol.UoTVersion = 0
	protocol.AcceptProxyProtocol = false
	protocol.DisableSNI = false
	protocol.Multiplex = ""
	protocol.UpMbps = 0
	protocol.DownMbps = 0
	protocol.SSRProtocol = ""
	protocol.ProtocolParam = ""
	protocol.ObfsParam = ""
	protocol.Encryption = ""
	protocol.EchEnable = false
	protocol.EchServerName = ""
	clearStreamTransport(protocol)
	clearLegacyObfs(protocol)
	clearQUICControls(protocol)
	clearEncryption(protocol)
}

func clearTLSClient(protocol *Protocol) {
	protocol.AllowInsecure = false
	protocol.Fingerprint = ""
}

func clearLegacyObfs(protocol *Protocol) {
	protocol.Obfs = ""
	protocol.ObfsHost = ""
	protocol.ObfsPath = ""
	protocol.ObfsPassword = ""
}

func clearStreamTransport(protocol *Protocol) {
	protocol.Transport = ""
	protocol.Host = ""
	protocol.Path = ""
	protocol.ServiceName = ""
	protocol.XhttpMode = ""
	protocol.XhttpExtra = ""
}

func clearQUICControls(protocol *Protocol) {
	protocol.HopPorts = ""
	protocol.HopInterval = 0
	protocol.UDPRelayMode = ""
	protocol.CongestionController = ""
	protocol.ReduceRtt = false
	protocol.PaddingScheme = ""
}
