package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type TelegramLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTelegramLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TelegramLogic {
	return &TelegramLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// HandleUpdatePayload decodes a raw webhook body and dispatches it. Decoding
// stays inside this package so the bot library's types never reach the HTTP
// layer, and a payload that fails to decode is reported to the caller instead
// of being dispatched as a zero update.
func (l *TelegramLogic) HandleUpdatePayload(payload []byte) error {
	var update models.Update
	if err := json.Unmarshal(payload, &update); err != nil {
		return errors.Wrap(xerr.NewErrCode(xerr.InvalidParams), "decode telegram update failed")
	}
	l.TelegramLogic(&update)
	return nil
}

func (l *TelegramLogic) TelegramLogic(req *models.Update) {
	if req.Message == nil || req.Message.Text == "" {
		l.Logger.Error("[TelegramLogic] Message is empty")
		return
	}
	cmd := messageCommand(req.Message)
	switch cmd {
	case "traffic":
		if err := l.traffic(req.Message.Chat.ID); err != nil {
			l.Logger.Error("[TelegramLogic] Traffic Error: ", zap.Any("error", err.Error()), zap.Any("command", cmd), zap.Any("chat_id", req.Message.Chat.ID))
		}
	case "bind":
		if err := l.bind(req.Message.Chat.ID, commandArguments(req.Message)); err != nil {
			l.Logger.Error("[TelegramLogic] Bind Error: ", zap.Any("error", err.Error()), zap.Any("command", cmd), zap.Any("chat_id", req.Message.Chat.ID))
		}
	case "start":
		if err := l.start(req); err != nil {
			l.Logger.Error("[TelegramLogic] Start Error: ", zap.Any("error", err.Error()), zap.Any("command", cmd), zap.Any("chat_id", req.Message.Chat.ID), zap.Any("text", req.Message.Text))
		}
	}
}

// sendMessage delivers plain text: command replies carry no formatting, and
// plain text cannot be broken by the data inside it.
func (l *TelegramLogic) sendMessage(message string, userId int64) error {
	bot := l.svcCtx.TelegramBot
	if bot == nil {
		return errors.New("telegram bot is not configured")
	}
	_, err := bot.SendMessage(context.Background(), &tgbot.SendMessageParams{
		ChatID: userId,
		Text:   message,
	})
	return err
}

// sendMarkdown delivers MarkdownV2 built by RenderMarkdownV2, which is the
// only place where message data is escaped.
func (l *TelegramLogic) sendMarkdown(message string, userId int64) error {
	bot := l.svcCtx.TelegramBot
	if bot == nil {
		return errors.New("telegram bot is not configured")
	}
	_, err := bot.SendMessage(context.Background(), &tgbot.SendMessageParams{
		ChatID:    userId,
		Text:      message,
		ParseMode: models.ParseModeMarkdown,
	})
	return err
}

func (l *TelegramLogic) traffic(userId int64) error {
	return nil
}

func (l *TelegramLogic) bind(userId int64, token string) error {
	return nil
}

func (l *TelegramLogic) start(req *models.Update) error {
	bindToken := commandArguments(req.Message)
	if bindToken == "" {
		return l.sendMessage("Please bind account!", req.Message.Chat.ID)
	}

	// The deep link carries a dedicated single-use bind token, never the
	// caller's session id: the link travels through Telegram chats and
	// screenshots, and a leaked session id would let its holder bind their
	// own Telegram account — and therefore log in — as this user.
	bindCacheKey := fmt.Sprintf("%v:%v", config.TelegramBindKey, bindToken)
	value, err := l.svcCtx.Redis.Get(context.Background(), bindCacheKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		l.Logger.Errorw("TelegramLogic start Redis Get Error: ", zap.Any("error", err.Error()), zap.Any("token", bindToken))
		return l.sendMessage("Bind failed!", req.Message.Chat.ID)
	}
	if value == "" {
		l.Logger.Errorw("TelegramLogic start Redis Get Error: ", zap.Any("error", "bind token not found"), zap.Any("token", bindToken))
		return l.sendMessage("Bind failed!", req.Message.Chat.ID)
	}
	userId, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		l.Logger.Errorw("TelegramLogic start ParseInt Error: ", zap.Any("error", err.Error()), zap.Any("token", bindToken))
		return l.sendMessage("Bind failed!", req.Message.Chat.ID)
	}

	method, err := l.svcCtx.Store.User().FindUserAuthMethodByPlatform(l.ctx, userId, "telegram")
	if err != nil && !ent.IsNotFound(err) {
		l.Logger.Errorw("TelegramLogic start FindUserAuthMethodByPlatform Error: ", zap.Any("error", err.Error()), zap.Any("userId", userId))
		return l.sendMessage("Bind failed!", req.Message.Chat.ID)
	}
	if ent.IsNotFound(err) {
		if err := l.svcCtx.Store.User().InsertUserAuthMethods(l.ctx, &user.AuthMethods{
			UserId:         userId,
			AuthType:       "telegram",
			AuthIdentifier: strconv.FormatInt(req.Message.Chat.ID, 10),
			Verified:       true,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}); err != nil {
			l.Logger.Errorw("TelegramLogic start InsertUserAuthMethod Error: ", zap.Any("error", err.Error()), zap.Any("userId", userId))
			return l.sendMessage("Bind failed!", req.Message.Chat.ID)
		}
	} else {
		method.AuthIdentifier = strconv.FormatInt(req.Message.Chat.ID, 10)
		if err := l.svcCtx.Store.User().UpdateUserAuthMethods(l.ctx, method); err != nil {
			l.Logger.Errorw("TelegramLogic start UpdateUserAuthMethod Error: ", zap.Any("error", err.Error()), zap.Any("userId", userId))
			return l.sendMessage("Bind failed!", req.Message.Chat.ID)
		}
	}
	// The token is spent the moment the binding lands, so a forwarded
	// link cannot rebind the same account to another chat.
	l.svcCtx.Redis.Del(context.Background(), bindCacheKey)
	text, err := RenderMarkdownV2(BindNotify, map[string]string{
		"Id":   strconv.FormatInt(userId, 10),
		"Time": time.Now().Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		l.Logger.Errorw("TelegramLogic start RenderTemplate Error: ", zap.Any("error", err.Error()))
		return l.sendMessage("Bound successfully!", req.Message.Chat.ID)
	}
	return l.sendMarkdown(text, req.Message.Chat.ID)
}
