package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	appconfig "github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/telegramsecret"
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
