package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	tgbot "github.com/go-telegram/bot"
	appconfig "github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/telegramsecret"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

// telegramWebhookRequest drives the handler through the same wrapper the
// router uses, so the assertions cover the real response the caller sees.
func telegramWebhookRequest(t *testing.T, svcCtx *svc.ServiceContext, secret string, body []byte) (int, []byte) {
	t.Helper()
	ctx := app.NewContext(0)
	ctx.Request.SetRequestURI("/v1/telegram/webhook")
	ctx.Request.Header.SetMethod(http.MethodPost)
	if secret != "" {
		ctx.Request.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
	}
	ctx.Request.SetBody(body)

	hertzx.Wrap(TelegramHandler(svcCtx))(context.Background(), ctx)
	return ctx.Response.StatusCode(), ctx.Response.Body()
}

// envelopeCode reports the result code of a response that carries a result
// envelope, and whether one was written at all.
func envelopeCode(t *testing.T, body []byte) (uint32, bool) {
	t.Helper()
	if len(body) == 0 {
		return 0, false
	}
	var response struct {
		Code uint32 `json:"code"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("unmarshal response %q: %v", body, err)
	}
	return response.Code, true
}

// bodyThatFailsToDecode is the probe used throughout: a payload the handler
// can only reject after it has passed the secret check and reached the
// decoder. A request that never gets that far answers with the success
// envelope instead, which makes the two outcomes distinguishable.
var bodyThatFailsToDecode = []byte(`{"update_id":`)

func telegramServiceContext(token string) *svc.ServiceContext {
	return &svc.ServiceContext{Config: appconfig.Config{Telegram: appconfig.Telegram{BotToken: token}}}
}

// assertDropped checks that the request was refused before it reached the
// decoder: HTTP 200 carrying the success envelope.
func assertDropped(t *testing.T, status int, body []byte, what string) {
	t.Helper()
	if status != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d", status, http.StatusOK)
	}
	code, ok := envelopeCode(t, body)
	if !ok {
		t.Fatalf("%s: no response envelope was written", what)
	}
	if code != 200 {
		t.Fatalf("%s: envelope code = %d, want 200", what, code)
	}
}

func TestMemberCanPairAndDisconnectAPrivateTelegramChat(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	owner, session := f.account(false)
	const member = "/v1/public/user"
	f.request(http.MethodGet, member+"/bind_telegram", session, "", xerr.ERROR)
	const token = "123456:pairing-fixture"
	f.service.Config.Telegram.BotToken = token
	f.service.Config.Telegram.BotName = "pairing_fixture_bot"
	type message struct{ chat, text string }
	var sent []message
	bot, err := tgbot.New(token, tgbot.WithSkipGetMe(), tgbot.WithHTTPClient(time.Second, &http.Client{Transport: providerTransport(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "/bot"+token+"/sendMessage", req.URL.Path)
		require.NoError(t, req.ParseMultipartForm(1<<20))
		defer req.MultipartForm.RemoveAll()
		sent = append(sent, message{req.FormValue("chat_id"), req.FormValue("text")})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"message_id":1}}`))}, nil
	})}))
	require.NoError(t, err)
	f.service.TelegramBot = bot
	RegisterTelegramHandlers(f.router, f.service)
	var link types.BindTelegramResponse
	before := time.Now()
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, member+"/bind_telegram", session, "", http.StatusOK), &link))
	deepLink, err := url.Parse(link.Url)
	require.NoError(t, err)
	require.Equal(t, "https", deepLink.Scheme)
	require.Equal(t, "t.me", deepLink.Host)
	require.Equal(t, "/pairing_fixture_bot", deepLink.Path)
	code := deepLink.Query().Get("start")
	require.NotEmpty(t, code)
	require.NotEqual(t, session, code)
	key := appconfig.TelegramBindKey + ":" + code
	ttl, err := f.service.Redis.PTTL(ctx, key).Result()
	require.NoError(t, err)
	require.Positive(t, ttl)
	require.WithinDuration(t, time.UnixMilli(link.ExpiredAt), before.Add(ttl), 2*time.Second)
	update := func(chat int, text string) {
		t.Helper()
		body := fmt.Sprintf(`{"message":{"chat":{"id":%d,"type":"private"},"from":{"id":%d},"text":%q,"entities":[{"type":"bot_command","offset":0,"length":6}]}}`, chat, chat, text)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/telegram/webhook", strings.NewReader(body))
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", telegramsecret.Derive(token))
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Empty(t, rec.Body.String())
	}
	update(104, "/start "+code)
	binding := f.client.UserAuthMethod.Query().OnlyX(ctx)
	require.Equal(t, owner.ID, binding.UserID)
	require.Equal(t, "telegram", binding.AuthType)
	require.Equal(t, "104", binding.AuthIdentifier)
	require.True(t, binding.Verified)
	require.Zero(t, f.service.Redis.Exists(ctx, key).Val())
	update(105, "/start "+code)
	require.Equal(t, binding.ID, f.client.UserAuthMethod.Query().OnlyX(ctx).ID)
	f.request(http.MethodPost, member+"/unbind_telegram", session, "", http.StatusOK)
	require.Zero(t, f.client.UserAuthMethod.Query().CountX(ctx))
	require.Len(t, sent, 3)
	require.Equal(t, "104", sent[0].chat)
	require.Contains(t, sent[0].text, "绑定")
	require.Equal(t, message{"105", "Bind failed!"}, sent[1])
	require.Equal(t, "104", sent[2].chat)
	require.Contains(t, sent[2].text, "解绑")
}

func TestTelegramWebhookRejectsAWrongSecretBeforeDecoding(t *testing.T) {
	status, body := telegramWebhookRequest(t, telegramServiceContext("bot-token"), "invalid", bodyThatFailsToDecode)
	assertDropped(t, status, body, "wrong secret")
}

func TestTelegramWebhookRejectsAMissingSecret(t *testing.T) {
	status, body := telegramWebhookRequest(t, telegramServiceContext("bot-token"), "", bodyThatFailsToDecode)
	assertDropped(t, status, body, "missing secret")
}

// The old scheme exposed md5(bot token) in the query string. Nothing derived
// from that scheme may be accepted now.
func TestTelegramWebhookRejectsTheLegacyQuerySecret(t *testing.T) {
	status, body := telegramWebhookRequest(t, telegramServiceContext("bot-token"), "e3f0a2d5d2a0f4dbb1c1a4f0a2e3c4d5", bodyThatFailsToDecode)
	assertDropped(t, status, body, "legacy query secret")
}

// An unconfigured bot must reject every request, including the secret an
// attacker can derive from the empty token.
func TestTelegramWebhookRejectsEverythingWhenTheBotIsUnconfigured(t *testing.T) {
	status, body := telegramWebhookRequest(t, telegramServiceContext(""), telegramsecret.Derive(""), bodyThatFailsToDecode)
	assertDropped(t, status, body, "unconfigured bot")
}

func TestTelegramWebhookDecodesThePayloadWhenTheSecretMatches(t *testing.T) {
	status, body := telegramWebhookRequest(t, telegramServiceContext("bot-token"), telegramsecret.Derive("bot-token"), bodyThatFailsToDecode)
	if status != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d", status, http.StatusOK)
	}
	code, ok := envelopeCode(t, body)
	if !ok {
		t.Fatal("no response envelope was written for an undecodable payload")
	}
	if code == 200 {
		t.Fatalf("envelope code = %d, want the decode failure to be reported to the caller", code)
	}
}

func TestTelegramWebhookAcceptsAnUpdateWithoutAMessage(t *testing.T) {
	// A valid secret and a payload that decodes but carries no message must be
	// accepted; the bot simply has nothing to answer.
	status, body := telegramWebhookRequest(t, telegramServiceContext("bot-token"), telegramsecret.Derive("bot-token"), []byte(`{"update_id":1}`))
	if status != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d", status, http.StatusOK)
	}
	if len(body) != 0 {
		t.Fatalf("body = %q, want no response envelope for an accepted update", body)
	}
}
