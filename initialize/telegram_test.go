package initialize

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/perfect-panel/server/internal/logic/telegram"
	"github.com/perfect-panel/server/internal/model/auth"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/telegramsecret"
	"github.com/stretchr/testify/require"
)

type telegramAuthModel struct {
	auth.Model
	method *auth.Auth
	err    error
}

func (model *telegramAuthModel) FindOneByMethod(context.Context, string) (*auth.Auth, error) {
	return model.method, model.err
}

type telegramStore struct {
	repository.Store
	auth *telegramAuthModel
}

func (store *telegramStore) Auth() auth.Model { return store.auth }

func TestTelegramInitializationHandlesDisabledAndInvalidConfiguration(t *testing.T) {
	t.Cleanup(func() { swapTelegramPoller(nil) })
	cases := []struct {
		name      string
		method    *auth.Auth
		lookupErr error
		wantErr   string
		wantNil   bool
	}{
		{name: "storage failure", lookupErr: errors.New("storage unavailable"), wantErr: "storage unavailable"},
		{name: "missing method", wantErr: "telegram authentication method not found"},
		{name: "disabled", method: &auth.Auth{Enabled: boolPtr(false), Config: `{"bot_token":"ignored"}`}, wantNil: true},
		{name: "invalid config", method: &auth.Auth{Enabled: boolPtr(true), Config: "not-json"}, wantErr: "invalid character"},
		{name: "empty token", method: &auth.Auth{Enabled: boolPtr(true), Config: `{}`}, wantNil: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service := &svc.ServiceContext{Store: &telegramStore{auth: &telegramAuthModel{method: test.method, err: test.lookupErr}}}
			service.Config.Telegram.BotToken = "stale-token"
			service.Config.Telegram.BotID = 42
			err := Telegram(service)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			if test.wantNil {
				require.Nil(t, service.TelegramBot)
				require.Empty(t, service.Config.Telegram.BotToken)
				require.Zero(t, service.Config.Telegram.BotID)
			}
		})
	}
}

type botTransport func(*http.Request) (*http.Response, error)

func (fn botTransport) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestWebhookReplacementPublishesOnlyAfterProviderConfirmation(t *testing.T) {
	model := &telegramAuthModel{method: &auth.Auth{Enabled: boolPtr(true)}}
	service := &svc.ServiceContext{Store: &telegramStore{auth: model}}
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	var reject, registeredURL, registeredSecret string
	http.DefaultTransport = botTransport(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Host != "api.telegram.org" {
			return nil, fmt.Errorf("unexpected Telegram endpoint %s", req.URL.Host)
		}
		_, method, ok := strings.Cut(strings.TrimPrefix(req.URL.Path, "/bot"), "/")
		if !ok {
			return nil, fmt.Errorf("missing Telegram method")
		}
		if req.Body != nil {
			defer req.Body.Close()
		}
		body := `{"ok":true,"result":true}`
		switch method {
		case "getMe":
			body = `{"ok":true,"result":{"id":42,"is_bot":true,"username":"support_bot"}}`
		case "setWebhook":
			if err := req.ParseMultipartForm(1 << 20); err != nil {
				return nil, err
			}
			defer req.MultipartForm.RemoveAll()
			if reject != method {
				registeredURL, registeredSecret = req.FormValue("url"), req.FormValue("secret_token")
				require.JSONEq(t, `["message"]`, req.FormValue("allowed_updates"))
				require.Equal(t, "1", req.FormValue("max_connections"))
			}
		case "deleteWebhook":
		default:
			return nil, fmt.Errorf("unexpected Telegram method %q", method)
		}
		if reject == method {
			body = `{"ok":false,"error_code":400,"description":"registration unavailable"}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	first := auth.TelegramAuthConfig{BotToken: "42:first", EnableNotify: true, WebHookDomain: "https://first.example.test/"}
	second := auth.TelegramAuthConfig{BotToken: "42:replacement", WebHookDomain: "https://second.example.test/"}
	model.method.Config = first.Marshal()
	require.NoError(t, Telegram(service))
	original, bot := service.Config.Telegram, service.TelegramBot
	require.NotNil(t, bot)
	require.EqualValues(t, 42, original.BotID)
	require.Equal(t, "support_bot", original.BotName)
	require.True(t, original.EnableNotify)
	model.method.Config = second.Marshal()
	for _, method := range []string{"getMe", "setWebhook", "deleteWebhook"} {
		reject = method
		service.Config.Debug = method == "deleteWebhook"
		require.ErrorContains(t, Telegram(service), "registration unavailable")
		require.Same(t, bot, service.TelegramBot)
		require.Equal(t, original, service.Config.Telegram)
		require.Equal(t, "https://first.example.test/v1/telegram/webhook", registeredURL)
		require.Equal(t, telegramsecret.Derive(first.BotToken), registeredSecret)
	}
	reject, service.Config.Debug = "", false
	require.NoError(t, Telegram(service))
	require.Equal(t, second.BotToken, service.Config.Telegram.BotToken)
	require.False(t, service.Config.Telegram.EnableNotify)
	require.Equal(t, "https://second.example.test/v1/telegram/webhook", registeredURL)
	require.Equal(t, telegramsecret.Derive(second.BotToken), registeredSecret)
	registeredURL, registeredSecret = "", ""
	require.NoError(t, telegram.SetWebhook(t.Context(), service))
	require.Equal(t, "https://second.example.test/v1/telegram/webhook", registeredURL)
	require.Equal(t, telegramsecret.Derive(second.BotToken), registeredSecret)
	model.method.Enabled = boolPtr(false)
	require.NoError(t, Telegram(service))
	require.Error(t, telegram.SetWebhook(t.Context(), service))
	require.Nil(t, service.TelegramBot)
}

func boolPtr(value bool) *bool { return &value }
