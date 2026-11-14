package tool

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// LegacyDefaultNodeSecret is the node secret the original seed data shipped. The
// value is public knowledge, so an installation still carrying it serves the
// node API -- which hands out every user's subscription uuid -- to anyone who
// guessed the published default.
const LegacyDefaultNodeSecret = "12345678"

// NodeSecretAlphabet keeps the generated secret URL-safe, because it travels as
// a query parameter on every node request.
const NodeSecretAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// NodeSecretLength yields about 190 bits over the 62-character alphabet.
const NodeSecretLength = 32

// GenerateNodeSecret draws uniformly from the URL-safe alphabet with crypto/rand.
// This value authenticates the node API, so a predictable generator would let
// anyone holding a few observed secrets predict the next one.
func GenerateNodeSecret(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("node secret length must be positive, got %d", length)
	}
	buf := make([]byte, length)
	limit := big.NewInt(int64(len(NodeSecretAlphabet)))
	for i := range buf {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("node secret entropy: %w", err)
		}
		buf[i] = NodeSecretAlphabet[n.Int64()]
	}
	return string(buf), nil
}
