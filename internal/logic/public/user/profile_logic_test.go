package user

import (
	"context"
	"errors"
	"testing"

	usermodel "github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/model/wallet"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
)

type profileUserModel struct {
	usermodel.Model
	updated *usermodel.User
	methods []*usermodel.AuthMethods
	err     error
}

func (model *profileUserModel) Update(_ context.Context, current *usermodel.User) error {
	model.updated = current
	return model.err
}
func (model *profileUserModel) FindUserAuthMethods(context.Context, int64) ([]*usermodel.AuthMethods, error) {
	return model.methods, model.err
}

type profileWalletModel struct {
	wallet.Model
	current *wallet.Wallet
}

func (model profileWalletModel) FindOne(context.Context, int64) (*wallet.Wallet, error) {
	return model.current, nil
}

type profileStore struct {
	repository.Store
	users   *profileUserModel
	wallets profileWalletModel
}

func (store profileStore) User() usermodel.Model { return store.users }
func (store profileStore) Wallet() wallet.Model  { return store.wallets }

func TestUserProfileMasksAndOrdersAuthMethodsAndReturnsWallet(t *testing.T) {
	account := &usermodel.User{
		Id: 19,
		AuthMethods: []usermodel.AuthMethods{
			{AuthType: "github", AuthIdentifier: "abcdef123456"},
			{AuthType: "mobile", AuthIdentifier: "+14155552671"},
			{AuthType: "email", AuthIdentifier: "person@example.test"},
			{AuthType: "custom", AuthIdentifier: "short"},
		},
	}
	store := profileStore{users: &profileUserModel{}, wallets: profileWalletModel{current: &wallet.Wallet{UserId: 19, Balance: 1200, GiftAmount: 300, Commission: 75}}}
	ctx := context.WithValue(t.Context(), constant.CtxKeyUser, account)
	got, err := NewQueryUserInfoLogic(ctx, &svc.ServiceContext{Store: store}).QueryUserInfo()
	if err != nil {
		t.Fatal(err)
	}
	if got.Balance != 1200 || got.GiftAmount != 300 || got.Commission != 75 || len(got.AuthMethods) != 4 {
		t.Fatalf("user profile = %+v", got)
	}
	want := []struct{ authType, identifier string }{
		{"email", "person@example.test"},
		{"mobile", "+1 415-***-2671"},
		{"github", "abc******456"},
		{"custom", "***"},
	}
	for i, item := range want {
		if got.AuthMethods[i].AuthType != item.authType || got.AuthMethods[i].AuthIdentifier != item.identifier {
			t.Errorf("auth method %d = %+v, want %s/%s", i, got.AuthMethods[i], item.authType, item.identifier)
		}
	}
}

func TestUserNotificationUpdateAndOAuthMethodAuthorization(t *testing.T) {
	account := &usermodel.User{Id: 23}
	users := &profileUserModel{}
	store := profileStore{users: users}
	service := &svc.ServiceContext{Store: store}
	login, balance, subscribe, trade := true, false, true, false
	ctx := context.WithValue(t.Context(), constant.CtxKeyUser, account)
	if err := NewUpdateUserNotifyLogic(ctx, service).UpdateUserNotify(&types.UpdateUserNotifyRequest{
		EnableLoginNotify: &login, EnableBalanceNotify: &balance, EnableSubscribeNotify: &subscribe, EnableTradeNotify: &trade,
	}); err != nil {
		t.Fatal(err)
	}
	if users.updated != account || account.EnableLoginNotify != &login || account.EnableBalanceNotify != &balance || account.EnableSubscribeNotify != &subscribe || account.EnableTradeNotify != &trade {
		t.Fatalf("notification preferences were not persisted: %+v", users.updated)
	}

	users.methods = []*usermodel.AuthMethods{{AuthType: "email", AuthIdentifier: "person@example.test"}}
	methods, err := NewGetOAuthMethodsLogic(ctx, service).GetOAuthMethods()
	if err != nil || len(methods.Methods) != 1 || methods.Methods[0].AuthIdentifier != "person@example.test" {
		t.Fatalf("OAuth methods = %+v, %v", methods, err)
	}
	if _, err := NewGetOAuthMethodsLogic(t.Context(), service).GetOAuthMethods(); err == nil {
		t.Fatal("OAuth methods were returned without an authenticated user")
	}
	if err := NewUpdateUserNotifyLogic(t.Context(), service).UpdateUserNotify(&types.UpdateUserNotifyRequest{}); err == nil {
		t.Fatal("notification settings were updated without an authenticated user")
	}
	if err := NewUpdateUserNotifyLogic(context.WithValue(t.Context(), constant.CtxKeyUser, &usermodel.User{}), service).UpdateUserNotify(&types.UpdateUserNotifyRequest{}); err == nil {
		t.Fatal("notification settings were updated for user ID zero")
	}

	users.err = errors.New("storage unavailable")
	if _, err := NewGetOAuthMethodsLogic(ctx, service).GetOAuthMethods(); err == nil {
		t.Fatal("OAuth lookup storage failure was ignored")
	}
	if err := NewUpdateUserNotifyLogic(ctx, service).UpdateUserNotify(&types.UpdateUserNotifyRequest{}); err == nil {
		t.Fatal("notification update storage failure was ignored")
	}
}
