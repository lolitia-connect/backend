package node

import (
	"strings"
	"testing"
)

const testCertPin = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

func TestNormalizeCertPinSHA256(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"lowercase hex", testCertPin, testCertPin},
		{"uppercase hex", strings.ToUpper(testCertPin), testCertPin},
		{"surrounding whitespace", "  " + testCertPin + "\n", testCertPin},
		{"empty", "", ""},
		{"too short", testCertPin[:63], ""},
		{"too long", testCertPin + "0", ""},
		{"non-hex characters", strings.Replace(testCertPin, "9", "g", 1), ""},
		{"colon separated", strings.Replace(testCertPin, "d0", ":d", 1), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeCertPinSHA256(tc.raw); got != tc.want {
				t.Fatalf("NormalizeCertPinSHA256(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestApplyReportedCertPinUpdatesSelfSignedProtocol(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{
		{Type: "vmess", Port: 443, Enable: true, Security: "tls", SNI: "example.com", CertMode: "self"},
		{Type: "trojan", Port: 444, Enable: true, Security: "tls", SNI: "example.com", CertMode: "self"},
	}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}

	changed, err := server.ApplyReportedCertPin("vmess", strings.ToUpper(testCertPin))
	if err != nil {
		t.Fatalf("ApplyReportedCertPin() error = %v", err)
	}
	if !changed {
		t.Fatal("ApplyReportedCertPin() changed = false, want true")
	}
	protocols, err := server.UnmarshalProtocols()
	if err != nil {
		t.Fatalf("UnmarshalProtocols() error = %v", err)
	}
	if protocols[0].CertPinSHA256 != testCertPin {
		t.Fatalf("vmess CertPinSHA256 = %q, want %q", protocols[0].CertPinSHA256, testCertPin)
	}
	if protocols[1].CertPinSHA256 != "" {
		t.Fatalf("trojan CertPinSHA256 = %q, want empty (other protocol types must not be touched)", protocols[1].CertPinSHA256)
	}

	// A repeated report with the same fingerprint must be a no-op so the
	// heartbeat does not invalidate the server cache every cycle.
	changed, err = server.ApplyReportedCertPin("vmess", testCertPin)
	if err != nil {
		t.Fatalf("ApplyReportedCertPin() repeat error = %v", err)
	}
	if changed {
		t.Fatal("ApplyReportedCertPin() repeat changed = true, want false")
	}
}

func TestApplyReportedCertPinMatchesProtocolTypeAliases(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{
		{Type: "hysteria", Port: 443, Enable: true, Security: "tls", SNI: "example.com", CertMode: "self"},
	}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}
	changed, err := server.ApplyReportedCertPin("hysteria2", testCertPin)
	if err != nil {
		t.Fatalf("ApplyReportedCertPin() error = %v", err)
	}
	if !changed {
		t.Fatal("ApplyReportedCertPin() changed = false, want true for hysteria2 alias")
	}
}

func TestApplyReportedCertPinIgnoresNonSelfCertMode(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{
		{Type: "vless", Port: 443, Enable: true, Security: "tls", SNI: "example.com", CertMode: "dns"},
	}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}
	changed, err := server.ApplyReportedCertPin("vless", testCertPin)
	if err != nil {
		t.Fatalf("ApplyReportedCertPin() error = %v", err)
	}
	if changed {
		t.Fatal("ApplyReportedCertPin() changed = true, want false for cert_mode=dns")
	}
}

func TestApplyReportedCertPinIgnoresInvalidFingerprint(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{
		{Type: "vmess", Port: 443, Enable: true, Security: "tls", SNI: "example.com", CertMode: "self"},
	}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}
	for _, raw := range []string{"", "not-a-fingerprint", testCertPin[:32]} {
		changed, err := server.ApplyReportedCertPin("vmess", raw)
		if err != nil {
			t.Fatalf("ApplyReportedCertPin(%q) error = %v", raw, err)
		}
		if changed {
			t.Fatalf("ApplyReportedCertPin(%q) changed = true, want false", raw)
		}
	}
}

// The pin is per protocol instance: a server can hold several self-signed
// inbounds of the same type and only the reporting one carries the fingerprint.
func TestApplyReportedCertPinUpdatesAllInstancesOfTheType(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{
		{Type: "vmess", Id: "1", Port: 443, Enable: true, Security: "tls", SNI: "example.com", CertMode: "self"},
		{Type: "vmess", Id: "2", Port: 8443, Enable: true, Security: "tls", SNI: "example.com", CertMode: "self"},
	}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}
	changed, err := server.ApplyReportedCertPin("vmess", testCertPin)
	if err != nil {
		t.Fatalf("ApplyReportedCertPin() error = %v", err)
	}
	if !changed {
		t.Fatal("ApplyReportedCertPin() changed = false, want true")
	}
	protocols, err := server.UnmarshalProtocols()
	if err != nil {
		t.Fatalf("UnmarshalProtocols() error = %v", err)
	}
	if len(protocols) != 2 || protocols[0].CertPinSHA256 != testCertPin || protocols[1].CertPinSHA256 != testCertPin {
		t.Fatalf("pins were not applied per instance: %#v", protocols)
	}
}

func TestCarryForwardCertPin(t *testing.T) {
	previous := []Protocol{
		{Type: "vmess", Id: "1", CertMode: "self", CertPinSHA256: testCertPin},
		{Type: "vmess", Id: "2", CertMode: "self", CertPinSHA256: testCertPin},
		{Type: "trojan", Id: "1", CertMode: "self"},
	}

	selfSigned := Protocol{Type: "vmess", Id: "1", CertMode: "self"}
	CarryForwardCertPin(previous, &selfSigned)
	if selfSigned.CertPinSHA256 != testCertPin {
		t.Fatalf("CertPinSHA256 = %q, want %q", selfSigned.CertPinSHA256, testCertPin)
	}

	otherInstance := Protocol{Type: "vmess", Id: "2", CertMode: "self"}
	CarryForwardCertPin(previous, &otherInstance)
	if otherInstance.CertPinSHA256 != testCertPin {
		t.Fatalf("second instance CertPinSHA256 = %q, want %q", otherInstance.CertPinSHA256, testCertPin)
	}

	// A different instance id shares no pin.
	freshInstance := Protocol{Type: "vmess", Id: "3", CertMode: "self"}
	CarryForwardCertPin(previous, &freshInstance)
	if freshInstance.CertPinSHA256 != "" {
		t.Fatalf("new instance CertPinSHA256 = %q, want empty", freshInstance.CertPinSHA256)
	}

	// Leaving cert_mode=self behind must not resurrect the pin.
	acme := Protocol{Type: "vmess", Id: "1", CertMode: "dns"}
	CarryForwardCertPin(previous, &acme)
	if acme.CertPinSHA256 != "" {
		t.Fatalf("cert_mode=dns CertPinSHA256 = %q, want empty", acme.CertPinSHA256)
	}
}
