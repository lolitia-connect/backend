package captcha

import (
	"context"

	"github.com/redis/go-redis/v9"
)

type CaptchaType string

const (
	CaptchaTypeLocal     CaptchaType = "local"
	CaptchaTypeTurnstile CaptchaType = "turnstile"
)

// Service defines the captcha service interface
type Service interface {
	// Generate generates a new captcha
	// For local captcha: returns id and base64 image
	// For turnstile: returns empty strings
	Generate(ctx context.Context) (id string, image string, err error)

	// Verify verifies the captcha
	// For local captcha: token is captcha id, code is user input
	// For turnstile: token is cf-turnstile-response, code is ignored
	Verify(ctx context.Context, token string, code string, ip string) (bool, error)

	// GetType returns the captcha type
	GetType() CaptchaType
}

// Config holds the configuration for captcha service
type Config struct {
	Type         CaptchaType
	RedisClient  *redis.Client
	TurnstileSecret string
}

// NewService creates a new captcha service based on the config
func NewService(config Config) Service {
	switch config.Type {
	case CaptchaTypeTurnstile:
		return newTurnstileService(config.TurnstileSecret)
	case CaptchaTypeLocal:
		fallthrough
	default:
		return newLocalService(config.RedisClient)
	}
}
