package captcha

import (
	"context"

	"github.com/redis/go-redis/v9"
)

type CaptchaType string

const (
	CaptchaTypeLocal     CaptchaType = "local"
	CaptchaTypeTurnstile CaptchaType = "turnstile"
	CaptchaTypeSlider    CaptchaType = "slider"
)

// Service defines the captcha service interface
type Service interface {
	// Generate generates a new captcha
	// For local captcha: returns id and base64 image
	// For turnstile: returns empty strings
	// For slider: returns id, background image, and block image (in image field as JSON)
	Generate(ctx context.Context) (id string, image string, err error)

	// Verify verifies the captcha
	// For local captcha: token is captcha id, code is user input
	// For turnstile: token is cf-turnstile-response, code is ignored
	// For slider: use VerifySlider instead
	Verify(ctx context.Context, token string, code string, ip string) (bool, error)

	// GetType returns the captcha type
	GetType() CaptchaType
}

// SliderService extends Service with slider-specific verification
type SliderService interface {
	Service
	// VerifySlider verifies slider position and trail, returns a one-time token on success
	VerifySlider(ctx context.Context, id string, x, y int, trail string) (token string, err error)
	// VerifySliderToken verifies the one-time token issued after slider verification
	VerifySliderToken(ctx context.Context, token string) (bool, error)
	// GenerateSlider returns id, background image base64, block image base64
	GenerateSlider(ctx context.Context) (id string, bgImage string, blockImage string, err error)
}

// Config holds the configuration for captcha service
type Config struct {
	Type            CaptchaType
	RedisClient     *redis.Client
	TurnstileSecret string
}

// NewService creates a new captcha service based on the config
func NewService(config Config) Service {
	switch config.Type {
	case CaptchaTypeTurnstile:
		return newTurnstileService(config.TurnstileSecret)
	case CaptchaTypeSlider:
		return newSliderService(config.RedisClient)
	case CaptchaTypeLocal:
		fallthrough
	default:
		return newLocalService(config.RedisClient)
	}
}

// NewSliderService creates a slider captcha service
func NewSliderService(redisClient *redis.Client) SliderService {
	return newSliderService(redisClient)
}
