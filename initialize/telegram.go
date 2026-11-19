package initialize

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/logic/telegram"
	"github.com/perfect-panel/server/internal/model/auth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/telegramsecret"
	"go.uber.org/zap"
)

// telegramPoll tracks the long-polling loop across re-initialisations. The
// previous poller must stop before a new bot starts, otherwise two getUpdates
// consumers race on the same token and Telegram rejects one with 409.
var telegramPoll struct {
	sync.Mutex
	cancel context.CancelFunc
}

// swapTelegramPoller cancels the previous polling loop and, when start is
// non-nil, installs the next one. Cancel and install share one critical
// section so two concurrent re-initialisations cannot leak a poller.
func swapTelegramPoller(start func(ctx context.Context)) {
	telegramPoll.Lock()
	defer telegramPoll.Unlock()
	if telegramPoll.cancel != nil {
		telegramPoll.cancel()
		telegramPoll.cancel = nil
	}
	if start != nil {
		ctx, cancel := context.WithCancel(context.Background())
		telegramPoll.cancel = cancel
		go start(ctx)
	}
}

// Telegram (re)initialises the bot from the stored configuration. On failure
// it returns without touching the running state, so a transient error during
// re-initialisation leaves the previous bot working instead of none at all.
func Telegram(svc *svc.ServiceContext) error {

	method, err := svc.Store.Auth().FindOneByMethod(context.Background(), "telegram")
	if err != nil {
		zap.S().Errorf("[Init Telegram Config] Get Telegram Config Error: %s", err.Error())
		return err
	}
	if method == nil {
		return fmt.Errorf("telegram authentication method not found")
	}
	if method.Enabled == nil || !*method.Enabled {
		swapTelegramPoller(nil)
		svc.Config.Telegram = config.Telegram{}
		svc.TelegramBot = nil
		zap.S().Debug("[Init Telegram Config] Telegram is disabled")
		return nil
	}
	tgConfig := new(auth.TelegramAuthConfig)
	if err = tgConfig.Unmarshal(method.Config); err != nil {
		zap.S().Errorf("[Init Telegram Config] Unmarshal Telegram Config Error: %s", err.Error())
		return err
	}

	if tgConfig.BotToken == "" {
		// The bot is deliberately unconfigured: stop a leftover poller.
		swapTelegramPoller(nil)
		svc.Config.Telegram = config.Telegram{}
		svc.TelegramBot = nil
		zap.S().Debug("[Init Telegram Config] Telegram Token is empty")
		return nil
	}

	bot, err := tgbot.New(tgConfig.BotToken,
		// GetMe runs explicitly below so its result can feed the config.
		tgbot.WithSkipGetMe(),
		// The previous library processed updates strictly in order; handlers
		// rely on that for double-submit protection (bind tokens), so keep
		// them synchronous.
		tgbot.WithNotAsyncHandlers(),
		// Only message updates are handled. Narrowing the subscription also
		// keeps unknown update payloads (which the library refuses to
		// decode) from ever reaching the polling loop.
		tgbot.WithAllowedUpdates(tgbot.AllowedUpdates{models.AllowedUpdateMessage}),
		tgbot.WithErrorsHandler(func(err error) {
			zap.S().Error("[Telegram Bot] update transport error", zap.Any("error", err.Error()))
		}),
		tgbot.WithDefaultHandler(func(ctx context.Context, _ *tgbot.Bot, update *models.Update) {
			if update.Message != nil {
				// Detach from the poller's lifetime: cancelling the poller on
				// re-initialisation must not abort an update mid-handling.
				telegram.NewTelegramLogic(context.WithoutCancel(ctx), svc).TelegramLogic(update)
			}
		}),
	)
	if err != nil {
		zap.S().Error("[Init Telegram Config] New Bot API Error: ", zap.Any("error", err.Error()))
		return err
	}

	// This runs synchronously inside startup and the admin settings request,
	// so it gets a deadline instead of the HTTP client's 60s default.
	getMeCtx, cancelGetMe := context.WithTimeout(context.Background(), 5*time.Second)
	user, err := bot.GetMe(getMeCtx)
	cancelGetMe()
	if err != nil {
		zap.S().Error("[Init Telegram Config] Get Bot Info Error: ", zap.Any("error", err.Error()))
		return err
	}

	newConfig := config.Telegram{
		Enable:        method.Enabled != nil && *method.Enabled,
		BotID:         user.ID,
		BotName:       user.Username,
		BotToken:      tgConfig.BotToken,
		EnableNotify:  tgConfig.EnableNotify,
		WebHookDomain: tgConfig.WebHookDomain,
	}

	// Pick mode: prefer webhook, fall back to long-polling when no domain or in debug.
	useWebhook := tgConfig.WebHookDomain != "" && !svc.Config.Debug
	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelSetup()
	if useWebhook {
		// Webhook mode: register the URL with Telegram. The secret travels in
		// the X-Telegram-Bot-Api-Secret-Token header, never in the URL. The
		// config is published only after Telegram accepted the registration,
		// so a failure here cannot leave the handler expecting a secret
		// Telegram does not send yet.
		webhookURL := strings.TrimRight(tgConfig.WebHookDomain, "/") + "/v1/telegram/webhook"
		if _, err = bot.SetWebhook(setupCtx, &tgbot.SetWebhookParams{
			URL:            webhookURL,
			SecretToken:    telegramsecret.Derive(tgConfig.BotToken),
			AllowedUpdates: []string{models.AllowedUpdateMessage},
			MaxConnections: 1,
		}); err != nil {
			zap.S().Errorf("[Init Telegram Config] Request Webhook Error: %s", err.Error())
			return err
		}
		swapTelegramPoller(nil)
		svc.Config.Telegram = newConfig
		svc.TelegramBot = bot
		zap.S().Info("[Init Telegram Config] Webhook registered", zap.Any("url", webhookURL))
	} else {
		// Long-polling mode. A leftover webhook registration blocks
		// getUpdates, so drop it first.
		if _, err = bot.DeleteWebhook(setupCtx, &tgbot.DeleteWebhookParams{}); err != nil {
			zap.S().Errorf("[Init Telegram Config] Delete Webhook Error: %s", err.Error())
			return err
		}
		// Publish the client and config before any update can arrive: the
		// update handlers reach the bot through svc.TelegramBot.
		svc.Config.Telegram = newConfig
		svc.TelegramBot = bot
		swapTelegramPoller(func(ctx context.Context) { bot.Start(ctx) })
		mode := "long-polling"
		if svc.Config.Debug {
			mode = "long-polling (debug)"
		}
		zap.S().Info("[Init Telegram Config] Using " + mode)
	}

	zap.S().Info("[Init Telegram Config] Telegram init success")
	return nil
}
