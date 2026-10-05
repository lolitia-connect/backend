package tool

import "crypto/subtle"

// SecretMatches reports whether the presented key authenticates against the
// configured node secret. An unprovisioned (empty) secret never authenticates:
// comparing it directly would let a bare `?secret_key=` through, which is worse
// than the well-known default it replaced. The comparison is constant time so a
// caller cannot walk the secret one byte at a time.
func SecretMatches(provided, expected string) bool {
	if expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
