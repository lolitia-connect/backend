package jwt

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
)

// TestNewJwtToken test NewJwtToken function
func TestParseJwtToken(t *testing.T) {
	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJEZXZpY2VJZCI6IjM4IiwiZXhwIjoxNzE4MTU2OTQ4LCJpYXQiOjE3MTc1NTIxNDgsInVzZXJJZCI6MX0.4W0nga82kNrfwWjkwcgYAWj4fI4iRc-ZftwVbu-a_kI"
	secret := "ae0536f9-6450-4606-8e13-5a19ed505da0"

	claims, err := ParseJwtToken(token, secret)
	if err != nil && !errors.Is(err, jwt.ErrTokenExpired) {
		t.Errorf("err: %v", err.Error())
		return
	}
	// parse jwt token success
	t.Logf("claims: %v", claims)
}

func TestRejectInvalidSigningMethodAndMissingExpiry(t *testing.T) {
	for _, token := range []*jwt.Token{
		jwt.NewWithClaims(jwt.SigningMethodHS384, jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix()}),
		jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"UserId": 1}),
	} {
		signed, err := token.SignedString([]byte("test-secret"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseJwtToken(signed, "test-secret"); err == nil {
			t.Fatal("invalid signing method or missing expiration accepted")
		}
	}
}

func TestMalformedSessionClaimsAreRejected(t *testing.T) {
	for _, claims := range []jwt.MapClaims{
		{}, {"UserId": "1", "SessionId": "session"}, {"UserId": 1.5, "SessionId": "session"},
		{"UserId": float64(1), "SessionId": 1}, {"UserId": float64(1), "SessionId": ""},
		{"UserId": float64(1), "SessionId": "session", "LoginType": 5},
	} {
		if _, _, _, err := SessionClaims(claims); err == nil {
			t.Fatalf("accepted malformed claims: %v", claims)
		}
	}
}
