package subscribe

import (
	"context"
	"strings"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tool"
	"go.uber.org/zap"
)

func IsUserAgentAllowed(ctx context.Context, svc *svc.ServiceContext, userAgent string) bool {
	if userAgent == "" {
		return false
	}

	keywords := tool.RemoveDuplicateElements(strings.Split(svc.Config.Subscribe.UserAgentList, "\n")...)
	clients, err := svc.Store.Client().List(ctx)
	if err != nil {
		zap.S().Errorw("[Subscribe] Query client list failed", zap.Any("error", err.Error()))
	}
	for _, item := range clients {
		keywords = append(keywords, item.UserAgent)
	}

	userAgent = strings.ToLower(userAgent)
	for _, keyword := range keywords {
		keyword = strings.ToLower(strings.TrimSpace(keyword))
		if keyword == "" {
			continue
		}
		if strings.Contains(userAgent, keyword) {
			return true
		}
	}
	return false
}
