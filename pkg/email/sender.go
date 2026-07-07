package email

import (
	"encoding/json"
	"fmt"

	"github.com/perfect-panel/server/pkg/email/smtp"
	"go.uber.org/zap"
)

type Sender interface {
	Send(to []string, subject, body string) error
}

func NewSender(platform, config, siteName string) (Sender, error) {
	switch parsePlatform(platform) {
	case SMTP:
		cfg := smtp.Config{}
		if err := json.Unmarshal([]byte(config), &cfg); err != nil {
			zap.S().Error("unmarshal email config failed", zap.Any("error", err.Error()), zap.Any("config", config))
			return nil, err
		}
		cfg.SiteName = siteName
		return smtp.NewClient(&cfg), nil
	default:
		return nil, fmt.Errorf("unsupported platform: %s", platform)
	}
}
