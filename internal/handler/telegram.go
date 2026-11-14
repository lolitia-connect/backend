package handler

import (
	"github.com/perfect-panel/server/internal/logic/telegram"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
	"github.com/perfect-panel/server/pkg/telegramsecret"
	"go.uber.org/zap"
)

func RegisterTelegramHandlers(router *hertzx.Engine, serverCtx *svc.ServiceContext) {
	router.POST("/v1/telegram/webhook", TelegramHandler(serverCtx))
}

func TelegramHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		token := svcCtx.Config.Telegram.BotToken
		if token == "" {
			// Without a configured bot nothing could legitimately be calling
			// this endpoint, and deriving a secret from an empty token would
			// accept whatever that derivation happens to produce.
			zap.L().Error("[TelegramHandler] Telegram bot token is not configured")
			c.Abort()
			result.HttpResult(c, nil, nil)
			return
		}
		// Telegram echoes the secret_token registered with setWebhook in this
		// header. It is derived from the bot token, so it rotates with it and
		// no credential ever has to be stored or logged.
		secret := c.GetHeader("X-Telegram-Bot-Api-Secret-Token")
		if !telegramsecret.Equal(secret, telegramsecret.Derive(token)) {
			zap.L().Error("[TelegramHandler] Webhook secret token mismatch")
			c.Abort()
			result.HttpResult(c, nil, nil)
			return
		}
		l := telegram.NewTelegramLogic(c.Request.Context(), svcCtx)
		if err := l.HandleUpdatePayload(c.Raw().Request.Body()); err != nil {
			zap.L().Error("[TelegramHandler] Failed to decode update", zap.Any("error", err.Error()))
			c.Abort()
			result.HttpResult(c, nil, err)
			return
		}
	}
}
