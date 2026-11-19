package jwt

import (
	"fmt"
	"math"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// Option jwt additional data
type Option struct {
	Key string
	Val any
}

// WithOption returns Option with key-value pairs
func WithOption(key string, val any) Option {
	return Option{
		Key: key,
		Val: val,
	}
}

// NewJwtToken Generate and return jwt token with given data.
func NewJwtToken(secretKey string, iat, seconds int64, opt ...Option) (string, error) {
	if secretKey == "" || seconds <= 0 {
		return "", fmt.Errorf("JWT secret and positive lifetime are required")
	}
	claims := make(jwt.MapClaims)
	claims["exp"] = iat + seconds
	claims["iat"] = iat

	for _, v := range opt {
		claims[v.Key] = v.Val
	}

	token := jwt.New(jwt.SigningMethodHS256)
	token.Claims = claims
	return token.SignedString([]byte(secretKey))
}

// ParseJwtToken Parse jwt token and return claims.
func ParseJwtToken(tokenString, secretKey string) (jwt.MapClaims, error) {
	if secretKey == "" {
		return nil, jwt.ErrTokenUnverifiable
	}
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return []byte(secretKey), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, jwt.ErrTokenInvalidId
	}
	return claims, nil
}

func SessionClaims(claims jwt.MapClaims) (int64, string, string, error) {
	userID, validID := claims["UserId"].(float64)
	sessionID, validSession := claims["SessionId"].(string)
	if !validID || math.IsNaN(userID) || math.IsInf(userID, 0) || userID <= 0 || userID >= math.Exp2(63) || math.Trunc(userID) != userID || !validSession || strings.TrimSpace(sessionID) == "" {
		return 0, "", "", jwt.ErrTokenInvalidClaims
	}
	loginType := ""
	if value, exists := claims["LoginType"]; exists {
		var valid bool
		loginType, valid = value.(string)
		if !valid {
			return 0, "", "", jwt.ErrTokenInvalidClaims
		}
	}
	return int64(userID), sessionID, loginType, nil
}
