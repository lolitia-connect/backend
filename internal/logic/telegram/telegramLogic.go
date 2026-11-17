package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tasklock"
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
	if req == nil || req.Message == nil || req.Message.Text == "" {
		return
	}
	if !privateSender(req.Message) {
		return
	}
	cmd := messageCommand(req.Message)
	switch cmd {
	case "traffic":
		if err := l.traffic(req.Message.Chat.ID); err != nil {
			l.Logger.Error("[TelegramLogic] Traffic Error: ", zap.Any("error", err.Error()), zap.Any("command", cmd), zap.Any("chat_id", req.Message.Chat.ID))
		}
	case "bind":
		if err := l.start(req); err != nil {
			l.Logger.Error("[TelegramLogic] Bind Error: ", zap.Any("error", err.Error()), zap.Any("command", cmd), zap.Any("chat_id", req.Message.Chat.ID))
		}
	case "start":
		if err := l.start(req); err != nil {
			l.Logger.Error("[TelegramLogic] Start Error: ", zap.Any("error", err.Error()), zap.Any("command", cmd), zap.Any("chat_id", req.Message.Chat.ID))
		}
	default:
		if cmd != "" {
			if err := l.adminCommand(req.Message); err != nil {
				l.Logger.Warnw("Telegram command failed", "command", cmd, "chat_id", req.Message.Chat.ID)
			}
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
	ctx, cancel := context.WithTimeout(l.ctx, 10*time.Second)
	defer cancel()
	_, err := bot.SendMessage(ctx, &tgbot.SendMessageParams{
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
	ctx, cancel := context.WithTimeout(l.ctx, 10*time.Second)
	defer cancel()
	_, err := bot.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:    userId,
		Text:      message,
		ParseMode: models.ParseModeMarkdown,
	})
	return err
}

func privateSender(message *models.Message) bool {
	return message != nil && message.Chat.Type == models.ChatTypePrivate && message.From != nil && message.From.ID > 0 && message.From.ID == message.Chat.ID
}

func (l *TelegramLogic) boundAccount(chatID int64) (*user.User, error) {
	binding, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, "telegram", strconv.FormatInt(chatID, 10))
	if err != nil {
		return nil, err
	}
	if binding == nil || !binding.Verified {
		return nil, xerr.NewErrCode(xerr.InvalidAccess)
	}
	account, err := l.svcCtx.Store.User().FindOne(l.ctx, binding.UserId)
	if err != nil {
		return nil, err
	}
	if account == nil || account.DeletedAt != nil || account.Enable == nil || !*account.Enable {
		return nil, xerr.NewErrCode(xerr.InvalidAccess)
	}
	return account, nil
}

func (l *TelegramLogic) traffic(chatID int64) error {
	account, err := l.boundAccount(chatID)
	if err != nil {
		return l.sendMessage("Please bind an enabled account first.", chatID)
	}
	subscriptions, err := l.svcCtx.Store.User().QueryUserSubscribe(l.ctx, account.Id, 0, 1)
	if err != nil {
		return err
	}
	var text strings.Builder
	fmt.Fprintf(&text, "Account %d\n", account.Id)
	for _, subscription := range subscriptions {
		name := "Subscription"
		if subscription.Subscribe != nil {
			name = subscription.Subscribe.Name
		}
		limit := fmt.Sprintf("%.2f GiB", float64(subscription.Traffic)/(1<<30))
		if subscription.TrafficUnlimited {
			limit = "unlimited"
		}
		expires := "no expiry"
		if subscription.ExpireTime.Unix() >= 31536000 {
			expires = subscription.ExpireTime.Format(time.DateOnly)
		}
		fmt.Fprintf(&text, "%s (#%d): %.2f / %s; %s\n", name, subscription.Id, float64(subscription.Upload)/(1<<30)+float64(subscription.Download)/(1<<30), limit, expires)
	}
	if len(subscriptions) == 0 {
		text.WriteString("No active subscriptions.")
	}
	return l.sendMessage(text.String(), chatID)
}

func (l *TelegramLogic) start(req *models.Update) error {
	if !privateSender(req.Message) {
		return nil
	}
	bindToken := commandArguments(req.Message)
	if bindToken == "" {
		return l.sendMessage("Please bind account!", req.Message.Chat.ID)
	}
	userId, err := l.redeemBinding(req.Message.Chat.ID, bindToken)
	if err != nil {
		l.Logger.Warnw("Telegram binding refused", "chat_id", req.Message.Chat.ID)
		return l.sendMessage("Bind failed!", req.Message.Chat.ID)
	}
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

var takeBindToken = redis.NewScript(`
local owner = redis.call('GET', KEYS[1])
if not owner then return false end
local ttl = redis.call('PTTL', KEYS[1])
redis.call('DEL', KEYS[1])
return {owner, ttl}
`)

func (l *TelegramLogic) redeemBinding(chatID int64, token string) (int64, error) {
	if l.svcCtx.Redis == nil || chatID <= 0 || token == "" || len(token) > 256 {
		return 0, xerr.NewErrCode(xerr.InvalidParams)
	}
	ctx, cancel := context.WithTimeout(l.ctx, 10*time.Second)
	defer cancel()
	key := fmt.Sprintf("%s:%s", config.TelegramBindKey, token)
	started := time.Now()
	result, err := takeBindToken.Run(ctx, l.svcCtx.Redis, []string{key}).Slice()
	if err != nil {
		return 0, err
	}
	if len(result) != 2 {
		return 0, xerr.NewErrCode(xerr.InvalidParams)
	}
	value, valueOK := result[0].(string)
	milliseconds, ttlOK := result[1].(int64)
	if !valueOK || !ttlOK || milliseconds <= 0 || milliseconds > int64((5*time.Minute)/time.Millisecond) {
		return 0, xerr.NewErrCode(xerr.InvalidParams)
	}
	userID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || userID <= 0 {
		return 0, xerr.NewErrCode(xerr.InvalidParams)
	}
	expiresAt := started.Add(time.Duration(milliseconds) * time.Millisecond)
	committed := false
	defer func() {
		if committed {
			return
		}
		remaining := time.Until(expiresAt)
		if remaining <= 0 {
			return
		}
		restoreCtx, restoreCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer restoreCancel()
		if err := l.svcCtx.Redis.SetNX(restoreCtx, key, value, remaining).Err(); err != nil {
			l.Logger.Errorw("Telegram binding token restore failed", "user_id", userID)
		}
	}()
	lock, acquired, err := tasklock.Acquire(ctx, l.svcCtx.Redis, fmt.Sprintf("telegram:bind:lock:%d", userID), 30*time.Second)
	if err != nil {
		return 0, err
	}
	if !acquired {
		return 0, xerr.NewErrCode(xerr.TooManyRequests)
	}
	defer lock.Release(ctx)
	account, err := l.svcCtx.Store.User().FindOne(ctx, userID)
	if err != nil {
		return 0, err
	}
	if account == nil || account.DeletedAt != nil || account.Enable == nil || !*account.Enable {
		return 0, xerr.NewErrCode(xerr.InvalidAccess)
	}
	method, err := l.svcCtx.Store.User().FindUserAuthMethodByPlatform(ctx, userID, "telegram")
	if err != nil && !ent.IsNotFound(err) {
		return 0, err
	}
	if method != nil {
		return 0, xerr.NewErrCode(xerr.InvalidParams)
	}
	if err := l.svcCtx.Store.User().InsertUserAuthMethods(ctx, &user.AuthMethods{
		UserId: userID, AuthType: "telegram", AuthIdentifier: strconv.FormatInt(chatID, 10), Verified: true,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		return 0, err
	}
	committed = true
	return userID, nil
}
