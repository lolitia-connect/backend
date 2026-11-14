package tool

import "testing"

func TestSecretMatches(t *testing.T) {
	tests := []struct {
		name     string
		provided string
		expected string
		want     bool
	}{
		{name: "exact match", provided: "s3cr3t", expected: "s3cr3t", want: true},
		{name: "wrong value", provided: "s3cr3t", expected: "other", want: false},
		{name: "case matters", provided: "S3cr3t", expected: "s3cr3t", want: false},
		{name: "prefix does not match", provided: "s3c", expected: "s3cr3t", want: false},
		{name: "superset does not match", provided: "s3cr3t!", expected: "s3cr3t", want: false},
		// An unprovisioned secret must not authenticate, least of all an empty
		// one: `?secret_key=` used to compare equal to it.
		{name: "empty expected rejects empty provided", provided: "", expected: "", want: false},
		{name: "empty expected rejects any provided", provided: "s3cr3t", expected: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SecretMatches(tt.provided, tt.expected); got != tt.want {
				t.Fatalf("SecretMatches(%q, %q) = %v, want %v", tt.provided, tt.expected, got, tt.want)
			}
		})
	}
}
