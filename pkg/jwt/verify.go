package jwt

import "github.com/golang-jwt/jwt/v5"

var (
	InvalidToken = jwt.ErrTokenInvalidId
	ExpiredToken = jwt.ErrTokenExpired
)

func VerifyToken(tokenString, secret string) error {
	_, err := ParseJwtToken(tokenString, secret)
	return err
}
