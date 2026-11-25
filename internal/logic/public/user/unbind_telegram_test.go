package user

import (
	"context"
	"errors"
	"testing"

	usermodel "github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

type unbindTelegramUserModel struct {
	usermodel.Model
	method     *usermodel.AuthMethods
	findErr    error
	deleteErr  error
	deletedFor int64
}

func (model *unbindTelegramUserModel) FindUserAuthMethodByPlatform(context.Context, int64, string) (*usermodel.AuthMethods, error) {
	return model.method, model.findErr
}

func (model *unbindTelegramUserModel) DeleteUserAuthMethods(_ context.Context, userID int64, platform string) error {
	if platform == "telegram" {
		model.deletedFor = userID
	}
	return model.deleteErr
}

type unbindTelegramStore struct {
	repository.Store
	users *unbindTelegramUserModel
}

func (store *unbindTelegramStore) User() usermodel.Model { return store.users }

func TestUnbindTelegramValidatesIdentityAndDeletesTheBinding(t *testing.T) {
	account := &usermodel.User{Id: 41}
	for _, test := range []struct {
		name      string
		method    *usermodel.AuthMethods
		findErr   error
		deleteErr error
		wantCode  uint32
		wantUser  int64
	}{
		{name: "missing user", wantCode: xerr.InvalidAccess},
		{name: "lookup failure", method: &usermodel.AuthMethods{AuthIdentifier: "77"}, findErr: errors.New("database unavailable"), wantCode: xerr.DatabaseQueryError},
		{name: "malformed chat id", method: &usermodel.AuthMethods{AuthIdentifier: "not-a-number"}, wantCode: xerr.ERROR},
		{name: "zero chat id", method: &usermodel.AuthMethods{AuthIdentifier: "0"}, wantCode: xerr.TelegramNotBound},
		{name: "delete failure", method: &usermodel.AuthMethods{AuthIdentifier: "77"}, deleteErr: errors.New("delete unavailable"), wantCode: xerr.DatabaseDeletedError, wantUser: account.Id},
		{name: "unbound", method: &usermodel.AuthMethods{AuthIdentifier: "77"}, wantUser: account.Id},
	} {
		t.Run(test.name, func(t *testing.T) {
			users := &unbindTelegramUserModel{method: test.method, findErr: test.findErr, deleteErr: test.deleteErr}
			service := &svc.ServiceContext{Store: &unbindTelegramStore{users: users}}
			ctx := t.Context()
			if test.name != "missing user" {
				ctx = context.WithValue(ctx, constant.CtxKeyUser, account)
			}
			err := NewUnbindTelegramLogic(ctx, service).UnbindTelegram()
			if test.wantCode == 0 {
				require.NoError(t, err)
				require.Equal(t, test.wantUser, users.deletedFor)
				return
			}
			var code *xerr.CodeError
			require.ErrorAs(t, err, &code)
			require.Equal(t, test.wantCode, code.GetErrCode())
			require.Equal(t, test.wantUser, users.deletedFor)
		})
	}
}
