package user

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
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
	ctx := t.Context()
	req.ReferCode = "new-code"
	req.Balance, req.GiftAmount, req.Commission = 1400, 180, 540
	var blocked string
	client.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			operation := m.Type()
			switch change := m.(type) {
			case *ent.SystemLogMutation:
				kind, _ := change.GetType()
				operation = map[log.Type]string{log.TypeBalance: "balance audit", log.TypeGift: "gift audit", log.TypeCommission: "commission audit"}[log.Type(kind)]
			case *ent.UserWalletMutation:
				if _, ok := change.Commission(); ok {
					operation = "commission update"
				}
			}
			if blocked != "" && operation == blocked {
				return nil, errors.New("adjustment storage unavailable")
			}
			return next.Mutate(ctx, m)
		})
	})
	for _, test := range []struct {
		operation string
		code      uint32
	}{
		{"balance audit", xerr.DatabaseInsertError}, {"gift audit", xerr.DatabaseInsertError}, {"commission audit", xerr.DatabaseInsertError},
		{ent.TypeUser, xerr.DatabaseUpdateError}, {ent.TypeUserWallet, xerr.DatabaseUpdateError}, {"commission update", xerr.DatabaseUpdateError},
	} {
		blocked = test.operation
		var failure *xerr.CodeError
		require.ErrorAs(t, logic.UpdateUserBasicInfo(&req), &failure, blocked)
		require.Equal(t, test.code, failure.GetErrCode(), blocked)
		require.Zero(t, client.SystemLog.Query().CountX(ctx), blocked)
		require.Equal(t, "referral", client.User.GetX(ctx, req.UserId).ReferCode, blocked)
		wallet := client.UserWallet.GetX(ctx, 9)
		require.Equal(t, [3]int64{1200, 300, 500}, [3]int64{wallet.Balance, wallet.GiftAmount, wallet.Commission}, blocked)
	}
	blocked = ""
	require.NoError(t, logic.UpdateUserBasicInfo(&req))
	require.Equal(t, req.ReferCode, client.User.GetX(ctx, req.UserId).ReferCode)
	wallet := client.UserWallet.GetX(ctx, 9)
	require.Equal(t, [3]int64{1400, 180, 540}, [3]int64{wallet.Balance, wallet.GiftAmount, wallet.Commission})
	require.NoError(t, logic.UpdateUserBasicInfo(&req))
	entries := client.SystemLog.Query().AllX(ctx)
	require.Len(t, entries, 3)
	amounts := make(map[log.Type]int64)
	for _, entry := range entries {
		require.Equal(t, req.UserId, entry.ObjectID)
		var receipt struct {
			Amount int64 `json:"amount"`
		}
		require.NoError(t, json.Unmarshal([]byte(entry.Content), &receipt))
		amounts[log.Type(entry.Type)] = receipt.Amount
	}
	require.Equal(t, map[log.Type]int64{log.TypeBalance: 200, log.TypeGift: -120, log.TypeCommission: 40}, amounts)
}
