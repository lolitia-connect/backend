package tool

import (
	"strings"
	"testing"
)

func TestGenerateNodeSecretIsUrlSafeAndSized(t *testing.T) {
	secret, err := GenerateNodeSecret(NodeSecretLength)
	if err != nil {
		t.Fatalf("GenerateNodeSecret error = %v", err)
	}
	if len(secret) != NodeSecretLength {
		t.Fatalf("secret has length %d, want %d", len(secret), NodeSecretLength)
	}
	for i, c := range secret {
		if !strings.ContainsRune(NodeSecretAlphabet, c) {
			t.Fatalf("secret %q has %q at %d, which is not URL safe", secret, c, i)
		}
	}
	if secret == LegacyDefaultNodeSecret {
		t.Fatalf("generated secret equals the well-known default")
	}
}

func TestGenerateNodeSecretVaries(t *testing.T) {
	const n = 64
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		secret, err := GenerateNodeSecret(NodeSecretLength)
		if err != nil {
			t.Fatalf("GenerateNodeSecret error = %v", err)
		}
		if _, dup := seen[secret]; dup {
			t.Fatalf("duplicate node secret after %d generations", i)
		}
		seen[secret] = struct{}{}
	}
}

func TestGenerateNodeSecretRejectsNonPositiveLength(t *testing.T) {
	if _, err := GenerateNodeSecret(0); err == nil {
		t.Fatal("GenerateNodeSecret(0) error = nil, want an error")
	}
}

// The boot-time check keys off this exact value, so a silent edit to it would
// stop reporting installations that still serve the published default.
func TestLegacyDefaultNodeSecretIsTheSeededValue(t *testing.T) {
	if LegacyDefaultNodeSecret != "12345678" {
		t.Fatalf("legacy default = %q, want the value the original seed shipped", LegacyDefaultNodeSecret)
	}
}
