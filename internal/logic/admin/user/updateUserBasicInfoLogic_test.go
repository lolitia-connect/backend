package user

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	_ "modernc.org/sqlite"
)

func basicInfoFixture(t *testing.T, avatar string) (*UpdateUserBasicInfoLogic, *ent.Client, types.UpdateUserBasiceInfoRequest) {
	t.Helper()
	database, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	client.User.Create().SetID(7).SetPassword("existing-password-hash").SetAvatar(avatar).
		SetEnable(true).SetIsAdmin(true).SetReferCode("referral").SetRefererID(3).
		SetReferralPercentage(15).SetOnlyFirstPurchase(true).SetEnableLoginNotify(true).SaveX(ctx)
	client.UserWallet.Create().SetID(9).SetUserID(7).SetBalance(1200).SetGiftAmount(300).SetCommission(500).SaveX(ctx)
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil)}
	request := types.UpdateUserBasiceInfoRequest{
		UserId: 7, Avatar: avatar, Balance: 1200, GiftAmount: 300, Commission: 500,
		ReferCode: "referral", RefererId: 3, ReferralPercentage: 15, OnlyFirstPurchase: true,
		Enable: true, IsAdmin: true,
	}
	return NewUpdateUserBasicInfoLogic(ctx, service), client, request
}

func TestUpdateUserBasicInfoTogglesAccountWithExistingAvatar(t *testing.T) {
	for _, avatar := range []string{"https://avatars.example.test/oauth-user.png", "", "data:image/png;base64,aW1hZ2U="} {
		t.Run(avatar, func(t *testing.T) {
			logic, client, request := basicInfoFixture(t, avatar)
			ctx := context.Background()
			for _, enabled := range []bool{false, true} {
				request.Enable = enabled
				if err := logic.UpdateUserBasicInfo(&request); err != nil {
					t.Fatalf("set enable=%t with unchanged avatar: %v", enabled, err)
				}
				stored := client.User.GetX(ctx, request.UserId)
				if stored.Enable != enabled || stored.Avatar != avatar || stored.Password != "existing-password-hash" || !stored.IsAdmin || !stored.EnableLoginNotify {
					t.Fatal("account status was not persisted or unrelated profile fields changed")
				}
				wallet := client.UserWallet.GetX(ctx, 9)
				if wallet.Balance != 1200 || wallet.GiftAmount != 300 || wallet.Commission != 500 || client.SystemLog.Query().CountX(ctx) != 0 {
					t.Fatal("toggling the account changed wallet funds or created money adjustment logs")
				}
			}
		})
	}
}

func TestUpdateUserBasicInfoValidatesOnlyChangedAvatar(t *testing.T) {
	for _, test := range []struct {
		name    string
		avatar  string
		wantErr bool
	}{
		{name: "new upload", avatar: "data:image/png;base64,aW1hZ2U="},
		{name: "clear avatar", avatar: ""},
		{name: "invalid new upload", avatar: "not-base64!", wantErr: true},
		{name: "oversized new upload", avatar: strings.Repeat("a", 2*1024*1024), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			const existingAvatar = "https://avatars.example.test/oauth-user.png"
			logic, client, request := basicInfoFixture(t, existingAvatar)
			request.Avatar, request.Enable = test.avatar, false
			err := logic.UpdateUserBasicInfo(&request)
			if (err != nil) != test.wantErr {
				t.Fatalf("update error = %v, want error = %t", err, test.wantErr)
			}
			stored := client.User.GetX(context.Background(), request.UserId)
			if test.wantErr {
				if stored.Avatar != existingAvatar || !stored.Enable {
					t.Fatal("invalid avatar update changed the account")
				}
			} else if stored.Avatar != test.avatar || stored.Enable {
				t.Fatal("valid avatar and account status update was not persisted")
			}
		})
	}
}

func TestWalletAdjustmentFailureRollsBackProfileAndAudit(t *testing.T) {
	logic, client, req := basicInfoFixture(t, "")
	req.ReferCode = "new-code"
	req.Balance++
	req.Commission++
	client.UserWallet.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if _, ok := m.(*ent.UserWalletMutation).Commission(); ok {
				return nil, errors.New("commission write failed")
			}
			return next.Mutate(ctx, m)
		})
	})
	if err := logic.UpdateUserBasicInfo(&req); err == nil {
		t.Fatal("adjustment unexpectedly succeeded")
	}
	if client.SystemLog.Query().CountX(t.Context()) != 0 || client.User.GetX(t.Context(), 7).ReferCode != "referral" || client.UserWallet.GetX(t.Context(), 9).Balance != 1200 {
		t.Fatal("partial wallet adjustment escaped rollback")
	}
}
