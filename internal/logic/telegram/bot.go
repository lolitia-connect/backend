package telegram

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/server/internal/model/auth"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/telegramsecret"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func GetTelegramConfig(ctx context.Context, svcCtx *svc.ServiceContext) (*types.TelegramConfig, error) {

	data, err := svcCtx.Store.Auth().FindOneByMethod(ctx, "telegram")
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get Telegram config failed: %v", err.Error())
	}
	if data == nil {
		return nil, xerr.NewErrCode(xerr.InvalidParams)
	}
	var telegramConfig auth.TelegramAuthConfig
	err = json.Unmarshal([]byte(data.Config), &telegramConfig)
	if err != nil {
		zap.S().Error("unmarshal telegram config failed", zap.Any("error", err.Error()))
		return nil, err
	}

	return &types.TelegramConfig{
		TelegramBotToken:      telegramConfig.BotToken,
		TelegramNotify:        data.Enabled != nil && *data.Enabled,
		TelegramWebHookDomain: telegramConfig.WebHookDomain,
	}, nil
}

func ApiLink(ctx context.Context, svcCtx *svc.ServiceContext, method string) string {
	cfg, err := GetTelegramConfig(ctx, svcCtx)
	if err != nil {
		return ""
	}
	return "https://api.telegram.org/bot" + cfg.TelegramBotToken + "/" + method
}

func SendUserMessage(ctx context.Context, svcCtx *svc.ServiceContext, u user.User, text string, parseMode string) {
	userTelegramChatId, ok := findTelegram(&u)
	if !ok || svcCtx.TelegramBot == nil || u.DeletedAt != nil || u.Enable == nil || !*u.Enable {
		return
	}
	if parseMode == "markdown" {
		text = strings.ReplaceAll(text, "_", "\\_")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := svcCtx.TelegramBot.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: userTelegramChatId, Text: text, ParseMode: models.ParseMode(parseMode)}); err != nil {
		zap.S().Warnw("Telegram notification failed", "user_id", u.Id)
	}
}

func SendAdminMessage(ctx context.Context, svcCtx *svc.ServiceContext, text string, parseMode string) {
	if svcCtx.TelegramBot == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	admins, err := svcCtx.Store.User().QueryAdminUsers(ctx)
	if err != nil {
		zap.S().Error("[SendAdminMessage] query admin users failed", zap.Any("error", err.Error()))
		return
	}
	for _, admin := range admins {
		if admin != nil {
			SendUserMessage(ctx, svcCtx, *admin, text, parseMode)
		}
	}
}

func SetWebhook(ctx context.Context, svcCtx *svc.ServiceContext) error {
	if svcCtx.TelegramBot == nil || svcCtx.Config.Telegram.WebHookDomain == "" {
		return xerr.NewErrCode(xerr.InvalidParams)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := svcCtx.TelegramBot.SetWebhook(ctx, &tgbot.SetWebhookParams{
		URL:            strings.TrimRight(svcCtx.Config.Telegram.WebHookDomain, "/") + "/v1/telegram/webhook",
		SecretToken:    telegramsecret.Derive(svcCtx.Config.Telegram.BotToken),
		AllowedUpdates: []string{models.AllowedUpdateMessage}, MaxConnections: 1,
	})
	return err
}

func findTelegram(u *user.User) (int64, bool) {
	for _, item := range u.AuthMethods {
		if item.AuthType == "telegram" {
			// string to int64
			parseInt, err := strconv.ParseInt(item.AuthIdentifier, 10, 64)
			if err != nil {
				return 0, false
			}
			return parseInt, true
		}

	}
	return 0, false
}
