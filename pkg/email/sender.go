package email

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/perfect-panel/server/pkg/email/smtp"
	"go.uber.org/zap"
)

type Sender interface {
	Send(to []string, subject, body string) error
}

func SendContext(ctx context.Context, sender Sender, to []string, subject, body string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if contextual, ok := sender.(interface {
		SendContext(context.Context, []string, string, string) error
	}); ok {
		return contextual.SendContext(ctx, to, subject, body)
	}
	return sender.Send(to, subject, body)
}

func NewSender(platform, config, siteName string) (Sender, error) {
	switch parsePlatform(platform) {
	case SMTP:
		cfg := smtp.Config{}
		if err := json.Unmarshal([]byte(config), &cfg); err != nil {
			zap.S().Error("unmarshal email config failed", zap.Any("error", err.Error()))
			return nil, err
		}
		cfg.SiteName = siteName
		return smtp.NewClient(&cfg), nil
	default:
		return nil, fmt.Errorf("unsupported platform: %s", platform)
	}
}
