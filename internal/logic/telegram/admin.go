package telegram

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"
	entticket "github.com/perfect-panel/server/ent/ticket"
	entuser "github.com/perfect-panel/server/ent/user"
	entsub "github.com/perfect-panel/server/ent/usersubscribe"
	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/ticket"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/usersession"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
)

type adminAction struct {
	Command  string `json:"command"`
	AdminID  int64  `json:"admin_id"`
	Target   int64  `json:"target"`
	Expected string `json:"expected"`
}

func adminActionKey(adminID int64, token string) string {
	return fmt.Sprintf("telegram:action:%d:%s", adminID, token)
}

func (l *TelegramLogic) saveAction(action adminAction) (string, error) {
	if l.svcCtx.Redis == nil {
		return "", xerr.NewErrCode(xerr.InvalidParams)
	}
	token := uuidx.SubscribeToken("")
	content, err := json.Marshal(action)
	if err != nil {
		return "", err
	}
	return token, l.svcCtx.Redis.Set(l.ctx, adminActionKey(action.AdminID, token), content, 5*time.Minute).Err()
}

var consumeAdminAction = redis.NewScript(`
local action = redis.call('GET', KEYS[1])
if not action then return false end
redis.call('DEL', KEYS[1])
return action
`)

func (l *TelegramLogic) takeAction(adminID int64, token string) (adminAction, error) {
	if l.svcCtx.Redis == nil || adminID <= 0 || len(token) != 32 {
		return adminAction{}, xerr.NewErrCode(xerr.InvalidParams)
	}
	content, err := consumeAdminAction.Run(l.ctx, l.svcCtx.Redis, []string{adminActionKey(adminID, token)}).Text()
	if err != nil {
		return adminAction{}, err
	}
	var action adminAction
	if err := json.Unmarshal([]byte(content), &action); err != nil || action.AdminID != adminID || action.Target <= 0 {
		return adminAction{}, xerr.NewErrCode(xerr.InvalidParams)
	}
	return action, nil
}

func expandAdminCommand(command, arguments string) (string, string) {
	for _, prefix := range []string{"user_sub", "user_log", "tickets", "tk", "rp", "close", "reopen", "user", "reset", "toggle", "ban"} {
		if value, ok := strings.CutPrefix(command, prefix+"_"); ok && value != "waiting" {
			return prefix, strings.TrimSpace(value + " " + arguments)
		}
	}
	return command, strings.TrimSpace(arguments)
}

func (l *TelegramLogic) adminCommand(message *models.Message) error {
	account, err := l.boundAccount(message.Chat.ID)
	if err != nil || account.IsAdmin == nil || !*account.IsAdmin {
		return l.sendMessage("Administrator access required.", message.Chat.ID)
	}
	command, arguments := expandAdminCommand(messageCommand(message), commandArguments(message))
	if token, ok := strings.CutPrefix(command, "confirm_"); ok {
		action, err := l.takeAction(account.Id, token)
		if err != nil {
			return l.sendMessage("Confirmation expired or unavailable.", message.Chat.ID)
		}
		if err := l.applyAction(message, account, action); err != nil {
			l.Logger.Warnw("Telegram administrator action failed", "command", action.Command, "admin_id", account.Id)
			return l.sendMessage("Operation failed or target changed. Request a new confirmation.", message.Chat.ID)
		}
		return l.sendMessage("Operation completed.", message.Chat.ID)
	}
	if token, ok := strings.CutPrefix(command, "cancel_"); ok {
		if l.svcCtx.Redis == nil || len(token) != 32 {
			return xerr.NewErrCode(xerr.InvalidParams)
		}
		if err := l.svcCtx.Redis.Del(l.ctx, adminActionKey(account.Id, token)).Err(); err != nil {
			return err
		}
		return l.sendMessage("Confirmation cancelled.", message.Chat.ID)
	}
	var text string
	switch command {
	case "help", "h":
		text = "/dash\n/tickets [page]\n/tickets_waiting\n/tk <id>\n/rp <id> <reply>\n/close <id>\n/reopen <id>\n/user <id|email>\n/user_sub <id|email>\n/user_log <id|email>\n/reset <subscription>\n/toggle <subscription>\n/ban <id|email>"
	case "dash":
		pending, ticketErr := l.svcCtx.Store.Ticket().QueryWaitReplyTotal(l.ctx)
		registered, userErr := l.svcCtx.Store.User().QueryResisterUserTotalByDate(l.ctx, time.Now())
		if ticketErr != nil || userErr != nil {
			return l.sendMessage("Dashboard temporarily unavailable.", message.Chat.ID)
		}
		text = fmt.Sprintf("Pending tickets: %d\nRegistered today: %d", pending, registered)
	case "tickets", "tickets_waiting":
		page, _ := strconv.Atoi(arguments)
		var status *uint8
		if command == "tickets_waiting" {
			pending := uint8(ticket.Pending)
			status = &pending
		}
		total, list, err := l.svcCtx.Store.Ticket().QueryTicketList(l.ctx, max(page, 1), 10, 0, status, "")
		if err != nil {
			return err
		}
		text = fmt.Sprintf("Tickets: %d\n", total)
		for _, current := range list {
			text += fmt.Sprintf("#%d [%d] %s\n", current.Id, current.Status, boundedText(current.Title, 100))
		}
	case "tk":
		id, _ := strconv.ParseInt(arguments, 10, 64)
		current, err := l.svcCtx.Store.Ticket().QueryTicketDetail(l.ctx, id)
		if err != nil || current == nil {
			return l.sendMessage("Ticket unavailable.", message.Chat.ID)
		}
		text = fmt.Sprintf("#%d [%d] %s\nAccount: %d\n%s\n", current.Id, current.Status, current.Title, current.UserId, boundedText(current.Description, 600))
		start := max(0, len(current.Follows)-5)
		for _, follow := range current.Follows[start:] {
			text += fmt.Sprintf("%s: %s\n", follow.From, boundedText(follow.Content, 300))
		}
	case "rp":
		idText, content, _ := strings.Cut(arguments, " ")
		id, _ := strconv.ParseInt(idText, 10, 64)
		if err := l.svcCtx.Store.Ticket().Reply(l.ctx, &ticket.Follow{TicketId: id, From: "admin", Type: ticket.FollowText, Content: content}, 0); err != nil {
			return l.sendMessage("Reply failed or ticket is closed.", message.Chat.ID)
		}
		l.recordAction(l.svcCtx.Store, message, account.Id, "ticket.reply", "ticket", id)
		text = "Reply saved."
	case "reopen":
		id, _ := strconv.ParseInt(arguments, 10, 64)
		if err := l.svcCtx.Store.Ticket().UpdateTicketStatus(l.ctx, id, 0, ticket.Pending); err != nil {
			return err
		}
		l.recordAction(l.svcCtx.Store, message, account.Id, "ticket.status", "ticket", id)
		text = "Ticket reopened."
	case "user", "user_sub", "user_log":
		target, err := l.lookupAccount(arguments)
		if err != nil || target == nil || target.DeletedAt != nil {
			return l.sendMessage("Account unavailable.", message.Chat.ID)
		}
		text = fmt.Sprintf("Account: %d\nEnabled: %t\n", target.Id, target.Enable != nil && *target.Enable)
		if command == "user_log" {
			entries, _, err := l.svcCtx.Store.Log().FilterSystemLog(l.ctx, &modellog.FilterParams{Page: 1, Size: 10, ObjectID: target.Id, Type: modellog.TypeLogin.Uint8()})
			if err != nil {
				return err
			}
			for _, entry := range entries {
				var login modellog.Login
				if login.Unmarshal([]byte(entry.Content)) == nil {
					text += fmt.Sprintf("%s %s %s\n", entry.CreatedAt.In(time.Local).Format(time.DateTime), login.LoginIP, login.Method)
				}
			}
		} else {
			subscriptions, err := l.svcCtx.Store.User().QueryUserSubscribe(l.ctx, target.Id)
			if err != nil {
				return err
			}
			for _, subscription := range subscriptions {
				text += fmt.Sprintf("Subscription #%d: status %d; upload %d; download %d\n", subscription.Id, subscription.Status, subscription.Upload, subscription.Download)
			}
		}
	case "close", "reset", "toggle", "ban":
		action, err := l.prepareAction(account.Id, command, arguments)
		if err != nil {
			return l.sendMessage("Target unavailable or operation unsupported.", message.Chat.ID)
		}
		token, err := l.saveAction(action)
		if err != nil {
			return err
		}
		text = fmt.Sprintf("Confirm %s for #%d:\n/confirm_%s\n/cancel_%s", command, action.Target, token, token)
	default:
		text = "Unknown command. Use /help."
	}
	return l.sendMessage(boundedText(text, 3900), message.Chat.ID)
}

func boundedText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return text
}

func (l *TelegramLogic) lookupAccount(value string) (*user.User, error) {
	if id, err := strconv.ParseInt(value, 10, 64); err == nil && id > 0 {
		return l.svcCtx.Store.User().FindOne(l.ctx, id)
	}
	return l.svcCtx.Store.User().FindOneByEmail(l.ctx, value)
}

func (l *TelegramLogic) prepareAction(adminID int64, command, value string) (adminAction, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	action := adminAction{AdminID: adminID, Command: command, Target: id}
	switch command {
	case "ban":
		account, err := l.lookupAccount(value)
		if err != nil {
			return action, err
		}
		if account == nil || account.Id == adminID || account.DeletedAt != nil {
			return action, xerr.NewErrCode(xerr.InvalidParams)
		}
		action.Target, action.Expected = account.Id, strconv.FormatBool(account.Enable != nil && *account.Enable)
	case "close":
		current, err := l.svcCtx.Store.Ticket().FindOne(l.ctx, id)
		if err != nil || current == nil {
			return action, xerr.NewErrCode(xerr.InvalidParams)
		}
		action.Expected = strconv.Itoa(int(current.Status))
	case "toggle", "reset":
		current, err := l.svcCtx.Store.User().FindOneSubscribe(l.ctx, id)
		if err != nil || current == nil || current.EntitlementSource != "" {
			return action, user.ErrProviderManaged
		}
		if command == "toggle" && current.Status != 1 && current.Status != 5 {
			return action, xerr.NewErrCode(xerr.InvalidParams)
		}
		action.Expected = strconv.Itoa(int(current.Status))
	default:
		return action, xerr.NewErrCode(xerr.InvalidParams)
	}
	if command != "ban" && (err != nil || id <= 0) {
		return action, xerr.NewErrCode(xerr.InvalidParams)
	}
	return action, nil
}

func (l *TelegramLogic) recordAction(store repository.Store, message *models.Message, adminID int64, action, object string, target int64) {
	entry := modellog.AdminAction{Action: action, Object: object, ObjectID: target, Source: "telegram"}
	entry.ActorID, entry.TelegramSenderID = adminID, message.From.ID
	modellog.RecordAdminAction(l.ctx, store.Log(), entry)
}

func (l *TelegramLogic) applyAction(message *models.Message, account *user.User, action adminAction) error {
	if !privateSender(message) || account == nil || account.Id != action.AdminID || account.IsAdmin == nil || !*account.IsAdmin || action.Target <= 0 {
		return xerr.NewErrCode(xerr.InvalidAccess)
	}
	var planID int64
	err := l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		authorized, err := store.Ent().User.Query().Where(entuser.ID(account.Id), entuser.Enable(true), entuser.IsAdmin(true), entuser.DeletedAtIsNil()).Exist(l.ctx)
		if err != nil {
			return err
		}
		if !authorized {
			return xerr.NewErrCode(xerr.InvalidAccess)
		}
		switch action.Command {
		case "close":
			expected, err := strconv.ParseUint(action.Expected, 10, 8)
			if err != nil {
				return xerr.NewErrCode(xerr.InvalidParams)
			}
			changed, err := store.Ent().Ticket.Update().Where(entticket.ID(action.Target), entticket.Status(uint8(expected))).SetStatus(ticket.Closed).Save(l.ctx)
			if err != nil {
				return err
			}
			if changed != 1 {
				return xerr.NewErrCode(xerr.InvalidParams)
			}
			l.recordAction(store, message, account.Id, "ticket.status", "ticket", action.Target)
		case "reset", "toggle":
			current, err := store.User().FindOneSubscribe(l.ctx, action.Target)
			if err != nil || current == nil || current.EntitlementSource != "" || strconv.Itoa(int(current.Status)) != action.Expected {
				return xerr.NewErrCode(xerr.InvalidParams)
			}
			planID = current.SubscribeId
			if action.Command == "reset" {
				changed, err := store.Ent().UserSubscribe.Update().Where(entsub.ID(current.Id), entsub.Status(current.Status), entsub.EntitlementSourceEQ("")).SetUpload(0).SetDownload(0).Save(l.ctx)
				if err != nil {
					return err
				}
				if changed != 1 {
					return xerr.NewErrCode(xerr.InvalidParams)
				}
			} else {
				if current.Status != 1 && current.Status != 5 {
					return xerr.NewErrCode(xerr.InvalidParams)
				}
				target := uint8(1)
				if current.Status == 1 {
					target = 5
				}
				changed, err := store.User().CompareSubscribeStatus(l.ctx, current.Id, current.Status, target)
				if err != nil {
					return err
				}
				if !changed {
					return xerr.NewErrCode(xerr.InvalidParams)
				}
			}
			l.recordAction(store, message, account.Id, "subscription."+action.Command, "user_subscribe", action.Target)
		case "ban":
			if action.Target == account.Id {
				return xerr.NewErrCode(xerr.InvalidParams)
			}
			expected, err := strconv.ParseBool(action.Expected)
			if err != nil {
				return err
			}
			changed, err := store.Ent().User.Update().Where(entuser.ID(action.Target), entuser.Enable(expected), entuser.DeletedAtIsNil()).SetEnable(!expected).Save(l.ctx)
			if err != nil {
				return err
			}
			if changed != 1 {
				return xerr.NewErrCode(xerr.InvalidParams)
			}
			l.recordAction(store, message, account.Id, "user.ban", "user", action.Target)
		default:
			return xerr.NewErrCode(xerr.InvalidParams)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if action.Command == "ban" {
		_, err = usersession.Rotate(l.ctx, l.svcCtx.Redis, action.Target)
		if err != nil {
			return err
		}
		return l.svcCtx.Store.Node().ClearServerAllCache(l.ctx)
	}
	if planID > 0 {
		return l.svcCtx.Store.Subscribe().ClearCache(l.ctx, planID)
	}
	return nil
}
