package captcha

import (
	"context"

	"github.com/perfect-panel/server/pkg/turnstile"
)

type turnstileService struct {
	service turnstile.Service
}

func newTurnstileService(secret string) Service {
	return &turnstileService{
		service: turnstile.New(turnstile.Config{
			Secret: secret,
		}),
	}
}

func (s *turnstileService) Generate(ctx context.Context) (id string, image string, err error) {
	// Turnstile doesn't need server-side generation
	return "", "", nil
}

func (s *turnstileService) Verify(ctx context.Context, token string, code string, ip string) (bool, error) {
	if token == "" {
		return false, nil
	}

	// Verify with Cloudflare Turnstile
	return s.service.Verify(ctx, token, ip)
}

func (s *turnstileService) GetType() CaptchaType {
	return CaptchaTypeTurnstile
}
