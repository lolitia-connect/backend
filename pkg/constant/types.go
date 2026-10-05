package constant

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Used for type cloning conversion
const (
	Int64   int64  = 0
	Uint32  uint32 = 0
	DevMode        = "dev"
)

// VerifyType is the type of verification code
type VerifyType uint8

const (
	Register VerifyType = iota + 1
	Security
)

func ParseVerifyType(i uint8) VerifyType {
	return VerifyType(i)
}

func (v VerifyType) String() string {
	switch v {
	case Register:
		return "register"
	case Security:
		return "security"
	default:
		return "unknown"
	}
}

// TempOrderCacheKey Cache to Redis Key
// eg: temp_order:order_no
const TempOrderCacheKey = "temp_order:%s"

type TemporaryOrderInfo struct {
	OrderNo string `json:"order_no"`
	// CheckoutToken is the guest's checkout capability. It is the only secret
	// that proves the caller created this order once the order number itself is
	// no longer enough to authorize a stream or a session exchange.
	CheckoutToken string `json:"checkout_token,omitempty"`
	Identifier    string `json:"identifier"`
	AuthType      string `json:"auth_type"`
	Password      string `json:"password"`
	InviteCode    string `json:"invite_code,omitempty"`
}

// CheckoutTokenHash returns the durable representation of a checkout
// capability. Only the caller ever holds the original token, so a leaked
// database or Redis snapshot cannot be replayed as a checkout capability.
func CheckoutTokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func (t *TemporaryOrderInfo) Unmarshal(data []byte) error {
	type Alias TemporaryOrderInfo
	aux := (*Alias)(t)
	return json.Unmarshal(data, aux)
}

func (t *TemporaryOrderInfo) Marshal() ([]byte, error) {
	type Alias TemporaryOrderInfo
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(t),
	})
}
