package telegram

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/config"
	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func commandMessage(text string, entityLength int) *models.Message {
	return &models.Message{
		Text: text,
		Entities: []models.MessageEntity{{
			Type:   models.MessageEntityTypeBotCommand,
			Offset: 0,
			Length: entityLength,
		}},
	}
}

func TestMessageCommandParsing(t *testing.T) {
	cases := []struct {
		name    string
		msg     *models.Message
		command string
		args    string
	}{
		{"command with args", commandMessage("/bind token-1", 5), "bind", "token-1"},
		{"command without args", commandMessage("/help", 5), "help", ""},
		{"mention is stripped", commandMessage("/start@my_bot abc", 13), "start", "abc"},
		{"plain text", &models.Message{Text: "hello"}, "", ""},
		{"nil message", nil, "", ""},
		{"entity not at offset zero", &models.Message{
			Text:     "hi /bind x",
			Entities: []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 3, Length: 5}},
		}, "", ""},
		{"entity length out of bounds", commandMessage("/x", 99), "", ""},
		{"forged entity without slash", commandMessage("bind token", 4), "", ""},
		{"multiple whitespace", commandMessage("/bind   token-1  ", 5), "bind", "token-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := messageCommand(tc.msg); got != tc.command {
				t.Fatalf("messageCommand = %q, want %q", got, tc.command)
			}
			if got := commandArguments(tc.msg); got != tc.args {
				t.Fatalf("commandArguments = %q, want %q", got, tc.args)
			}
		})
	}
}

func TestPrivateSenderBoundary(t *testing.T) {
	for _, message := range []*models.Message{nil, {Chat: models.Chat{ID: 100, Type: models.ChatTypeGroup}, From: &models.User{ID: 100}}, {Chat: models.Chat{ID: 100, Type: models.ChatTypePrivate}, From: &models.User{ID: 101}}, {Chat: models.Chat{ID: 100, Type: models.ChatTypePrivate}}} {
		if privateSender(message) {
			t.Fatal("non-private sender accepted")
		}
	}
	if !privateSender(&models.Message{Chat: models.Chat{ID: 100, Type: models.ChatTypePrivate}, From: &models.User{ID: 100}}) {
		t.Fatal("private sender rejected")
	}
}

var bindingFixtureID atomic.Uint64

func bindingFixture(t *testing.T) (*TelegramLogic, *ent.Client, *redis.Client, *miniredis.Miniredis) {
	t.Helper()
	database, err := sql.Open("sqlite", fmt.Sprintf("file:binding-%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", bindingFixtureID.Add(1)))
	if err != nil {
		t.Fatal(err)
	}
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	mini := miniredis.RunT(t)
	cache := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	client.User.Create().SetID(7).SetPassword("fixture-password").SetEnable(true).SaveX(context.Background())
	service := &svc.ServiceContext{Store: repository.NewStore(client, cache), Redis: cache}
	return NewTelegramLogic(context.Background(), service), client, cache, mini
}

type botRequestFunc func(*http.Request) (*http.Response, error)

func (f botRequestFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestTelegramWebhookBindsPrivateChatAndShowsOnlyItsTraffic(t *testing.T) {
	logic, client, cache, _ := bindingFixture(t)
	ctx := t.Context()
	type reply struct{ chat, text, mode string }
	var replies []reply
	bot, err := tgbot.New("123456:fixture", tgbot.WithSkipGetMe(), tgbot.WithHTTPClient(time.Second, botRequestFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, "/sendMessage") {
			t.Fatalf("unexpected bot request: %s %s", request.Method, request.URL.Path)
		}
		if err := request.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		defer request.MultipartForm.RemoveAll()
		replies = append(replies, reply{request.FormValue("chat_id"), request.FormValue("text"), request.FormValue("parse_mode")})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"message_id":1}}`))}, nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	logic.svcCtx.TelegramBot = bot
	owner := client.User.GetX(ctx, 7)
	other := client.User.Create().SetPassword("fixture").SaveX(ctx)
	plan := client.Subscribe.Create().SetName("Regional plan").SaveX(ctx)
	otherPlan := client.Subscribe.Create().SetName("Other account plan").SaveX(ctx)
	for i, item := range []struct{ ownerID, planID int64 }{{owner.ID, plan.ID}, {other.ID, otherPlan.ID}} {
		client.UserSubscribe.Create().SetUserID(item.ownerID).SetSubscribeID(item.planID).SetOrderID(int64(i + 1)).
			SetStatus(1).SetExpireTime(time.Now().Add(24 * time.Hour)).SetTraffic(8 << 30).
			SetUpload(1 << 30).SetDownload(2 << 30).SetToken(fmt.Sprintf("subscription-%d", i)).SaveX(ctx)
	}
	const chatID = 104
	const key = config.TelegramBindKey + ":webhook-fixture"
	if err := cache.Set(ctx, key, owner.ID, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	update := func(chatType, text string, commandLength int) {
		t.Helper()
		body := fmt.Sprintf(`{"message":{"chat":{"id":%d,"type":%q},"from":{"id":%d},"text":%q,"entities":[{"type":"bot_command","offset":0,"length":%d}]}}`, chatID, chatType, chatID, text, commandLength)
		if err := logic.HandleUpdatePayload([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	update("group", "/bind webhook-fixture", 5)
	if len(replies) != 0 || cache.Exists(ctx, key).Val() != 1 {
		t.Fatal("a non-private chat consumed the bind token or received a reply")
	}
	update("private", "/traffic", 8)
	update("private", "/start webhook-fixture", 6)
	update("private", "/traffic", 8)
	update("private", "/bind webhook-fixture", 5)
	if len(replies) != 4 {
		t.Fatalf("received %d bot replies, want four", len(replies))
	}
	for _, response := range replies {
		if response.chat != "104" {
			t.Fatalf("reply sent to chat %q", response.chat)
		}
	}
	if replies[0].text != "Please bind an enabled account first." || replies[1].mode == "" ||
		!strings.Contains(replies[2].text, "Regional plan") || !strings.Contains(replies[2].text, "3.00 / 8.00 GiB") ||
		strings.Contains(replies[2].text, "Other account plan") || replies[3].text != "Bind failed!" {
		t.Fatalf("unexpected bind and traffic replies: %+v", replies)
	}
	binding, err := logic.svcCtx.Store.User().FindUserAuthMethodByOpenID(ctx, "telegram", "104")
	if err != nil || binding == nil || !binding.Verified || binding.UserId != owner.ID {
		t.Fatalf("webhook did not verify the chat's account binding: %v", err)
	}
}

func TestBindTokenCanOnlyBeRedeemedOnce(t *testing.T) {
	logic, client, cache, _ := bindingFixture(t)
	ctx := context.Background()
	key := config.TelegramBindKey + ":once"
	if err := cache.Set(ctx, key, 7, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	var completed atomic.Int32
	var workers sync.WaitGroup
	for index := 0; index < 24; index++ {
		workers.Add(1)
		go func(chatID int64) {
			defer workers.Done()
			if _, err := logic.redeemBinding(chatID, "once"); err == nil {
				completed.Add(1)
			}
		}(int64(100 + index))
	}
	workers.Wait()
	if completed.Load() != 1 || client.UserAuthMethod.Query().CountX(ctx) != 1 || cache.Exists(ctx, key).Val() != 0 {
		t.Fatalf("binding was not one-use: success=%d, rows=%d", completed.Load(), client.UserAuthMethod.Query().CountX(ctx))
	}
}

func TestDifferentBindTokensCannotRebindAccount(t *testing.T) {
	logic, client, cache, _ := bindingFixture(t)
	ctx := context.Background()
	for _, token := range []string{"first", "second"} {
		if err := cache.Set(ctx, config.TelegramBindKey+":"+token, 7, 45*time.Second).Err(); err != nil {
			t.Fatal(err)
		}
	}
	var completed atomic.Int32
	var workers sync.WaitGroup
	for index, token := range []string{"first", "second"} {
		workers.Add(1)
		go func(chatID int64, token string) {
			defer workers.Done()
			if _, err := logic.redeemBinding(chatID, token); err == nil {
				completed.Add(1)
			}
		}(int64(100+index), token)
	}
	workers.Wait()
	if completed.Load() != 1 || client.UserAuthMethod.Query().CountX(ctx) != 1 {
		t.Fatalf("account rebound concurrently: success=%d", completed.Load())
	}
}

func TestFailedBindingRestoresOnlyRemainingLifetime(t *testing.T) {
	logic, _, cache, mini := bindingFixture(t)
	ctx := context.Background()
	key := config.TelegramBindKey + ":missing-owner"
	if err := cache.Set(ctx, key, 999, 45*time.Second).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := logic.redeemBinding(100, "missing-owner"); err == nil {
		t.Fatal("missing owner was bound")
	}
	remaining, err := cache.PTTL(ctx, key).Result()
	if err != nil || remaining <= 0 || remaining > 45*time.Second || cache.Get(ctx, key).Val() != "999" {
		t.Fatalf("token lifetime changed: %v, %v", remaining, err)
	}
	mini.FastForward(45 * time.Second)
	if cache.Exists(ctx, key).Val() != 0 {
		t.Fatal("restored token outlived the original link")
	}
}

func TestAdminConfirmationIssuerConcurrencyAndExpiry(t *testing.T) {
	logic, _, cache, mini := bindingFixture(t)
	action := adminAction{AdminID: 7, Command: "close", Target: 99, Expected: "1"}
	token, err := logic.saveAction(action)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := logic.takeAction(8, token); err == nil {
		t.Fatal("another administrator consumed the action")
	}
	if cache.Exists(logic.ctx, adminActionKey(7, token)).Val() != 1 {
		t.Fatal("wrong issuer removed the action")
	}
	var completed atomic.Int32
	var workers sync.WaitGroup
	for index := 0; index < 24; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if actual, err := logic.takeAction(7, token); err == nil && actual == action {
				completed.Add(1)
			}
		}()
	}
	workers.Wait()
	if completed.Load() != 1 {
		t.Fatalf("confirmation consumed %d times", completed.Load())
	}
	token, err = logic.saveAction(action)
	if err != nil {
		t.Fatal(err)
	}
	mini.FastForward(5 * time.Minute)
	if _, err := logic.takeAction(7, token); err == nil {
		t.Fatal("expired action accepted")
	}
}

func TestAdminActionRechecksAuthorityAndTargetState(t *testing.T) {
	logic, client, _, _ := bindingFixture(t)
	ctx := logic.ctx
	client.User.UpdateOneID(7).SetIsAdmin(true).ExecX(ctx)
	account, err := logic.svcCtx.Store.User().FindOne(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	message := &models.Message{Chat: models.Chat{ID: 100, Type: models.ChatTypePrivate}, From: &models.User{ID: 100}}
	current := client.Ticket.Create().SetUserID(8).SetStatus(1).SaveX(ctx)
	action, err := logic.prepareAction(7, "close", fmt.Sprint(current.ID))
	if err != nil {
		t.Fatal(err)
	}
	client.Ticket.UpdateOneID(current.ID).SetStatus(2).ExecX(ctx)
	if err := logic.applyAction(message, account, action); err == nil {
		t.Fatal("stale status confirmation accepted")
	}
	if client.Ticket.GetX(ctx, current.ID).Status != 2 {
		t.Fatal("stale action changed the ticket")
	}
	action.Expected = "2"
	client.User.UpdateOneID(7).SetIsAdmin(false).ExecX(ctx)
	if err := logic.applyAction(message, account, action); err == nil {
		t.Fatal("revoked administrator applied action")
	}
	client.User.UpdateOneID(7).SetIsAdmin(true).ExecX(ctx)
	if err := logic.applyAction(message, account, action); err != nil {
		t.Fatal(err)
	}
	if client.Ticket.GetX(ctx, current.ID).Status != 4 {
		t.Fatal("confirmed close not applied")
	}
	entries, count, err := logic.svcCtx.Store.Log().FilterSystemLog(ctx, &modellog.FilterParams{Type: modellog.TypeAdminAction.Uint8()})
	if err != nil || count != 1 || entries[0].ObjectID != 7 {
		t.Fatalf("missing actor audit: %d, %v", count, err)
	}
	if _, err := logic.prepareAction(7, "ban", "7"); err == nil {
		t.Fatal("self-ban accepted")
	}
	for _, status := range []uint8{0, 2, 3, 4} {
		subscription := client.UserSubscribe.Create().SetUserID(8).SetOrderID(1).SetSubscribeID(1).SetStatus(status).SaveX(ctx)
		if _, err := logic.prepareAction(7, "toggle", fmt.Sprint(subscription.ID)); err == nil {
			t.Fatal("finished subscription can be enabled")
		}
	}
	provider := client.UserSubscribe.Create().SetUserID(8).SetOrderID(1).SetSubscribeID(1).SetStatus(1).SetEntitlementSource("apple").SaveX(ctx)
	if _, err := logic.prepareAction(7, "reset", fmt.Sprint(provider.ID)); err != user.ErrProviderManaged {
		t.Fatalf("provider reset not rejected: %v", err)
	}
}
