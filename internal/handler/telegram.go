package handler

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/perfect-panel/server/internal/logic/telegram"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
	"github.com/perfect-panel/server/pkg/tool"
	"go.uber.org/zap"
)

func RegisterTelegramHandlers(router *hertzx.Engine, serverCtx *svc.ServiceContext) {
	router.POST("/v1/telegram/webhook", TelegramHandler(serverCtx))
}

func TelegramHandler(svcCtx *svc.ServiceContext) func(c *hertzx.Context) {
	return func(c *hertzx.Context) {
		// auth secret
		secret := c.Query("secret")
		if secret != tool.Md5Encode(svcCtx.Config.Telegram.BotToken, false) {
			zap.L().Error("[TelegramHandler] Secret is wrong", zap.Any("request secret", secret), zap.Any("config secret", tool.Md5Encode(svcCtx.Config.Telegram.BotToken, false)), zap.Any("token", svcCtx.Config.Telegram.BotToken))
			c.Abort()
			result.HttpResult(c, nil, nil)
			return
		}
		var request tgbotapi.Update
		if err := c.BindJSON(&request); err != nil {
			zap.L().Error("[TelegramHandler] Failed to bind request", zap.Any("error", err.Error()))
			c.Abort()
			result.HttpResult(c, nil, err)
		}
		l := telegram.NewTelegramLogic(c.Request.Context(), svcCtx)
		l.TelegramLogic(&request)
	}
}
