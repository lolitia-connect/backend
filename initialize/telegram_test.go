package initialize

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/server/internal/model/auth"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
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

func boolPtr(value bool) *bool { return &value }
