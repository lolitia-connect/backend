package order

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/ent/orderevent"
	modelorder "github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func TestValidateV2CreateRequestBalancePayment(t *testing.T) {
	account := &user.User{Id: 7}
	guest := &types.V2GuestOrderRequest{AuthType: "email", Identifier: "guest@example.com", Password: "password123"}
	for _, tc := range []struct {
		name    string
		request *types.V2CreateOrderRequest
		user    *user.User
		wantErr bool
	}{
		{name: "purchase", request: &types.V2CreateOrderRequest{Type: "purchase", PaymentID: -1, SubscribeID: 2, Quantity: 1}, user: account},
		{name: "renewal", request: &types.V2CreateOrderRequest{Type: "renewal", PaymentID: -1, UserSubscribeID: 3, Quantity: 1}, user: account},
		{name: "traffic reset", request: &types.V2CreateOrderRequest{Type: "reset_traffic", PaymentID: -1, UserSubscribeID: 3}, user: account},
		{name: "normalized type", request: &types.V2CreateOrderRequest{Type: " PURCHASE ", PaymentID: -1, SubscribeID: 2, Quantity: 1}, user: account},
		{name: "guest purchase", request: &types.V2CreateOrderRequest{Type: "purchase", PaymentID: -1, SubscribeID: 2, Quantity: 1, Guest: guest}, wantErr: true},
		{name: "recharge", request: &types.V2CreateOrderRequest{Type: " ReCharge ", PaymentID: -1, Amount: 100}, user: account, wantErr: true},
		{name: "missing payment", request: &types.V2CreateOrderRequest{Type: "purchase", SubscribeID: 2, Quantity: 1}, user: account, wantErr: true},
		{name: "other negative payment", request: &types.V2CreateOrderRequest{Type: "purchase", PaymentID: -2, SubscribeID: 2, Quantity: 1}, user: account, wantErr: true},
		{name: "missing request", user: account, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateV2CreateRequest(tc.request, tc.user)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("balance payment rejected: %v", err)
				}
				return
			}
			var code *xerr.CodeError
			if !errors.As(err, &code) || code.GetErrCode() != xerr.InvalidParams {
				t.Fatalf("validation error = %v, want InvalidParams", err)
			}
		})
	}
}

func TestV2BalancePaymentCreatesPaidOrderAndReplaysWithoutDebitingAgain(t *testing.T) {
	for _, orderType := range []string{"purchase", "renewal", "reset_traffic"} {
		t.Run(orderType, func(t *testing.T) {
			ctx := t.Context()
			db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = db.Close() })
			client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
			addr := miniredis.RunT(t).Addr()
			cache := redis.NewClient(&redis.Options{Addr: addr})
			queue := asynq.NewClient(asynq.RedisClientOpt{Addr: addr})
			t.Cleanup(func() { _ = cache.Close(); _ = queue.Close() })
			service := &svc.ServiceContext{Store: repository.NewStore(client, cache), Redis: cache, Queue: queue}
			service.Config.JwtAuth.AccessSecret = "balance-payment-test-secret"
			account := client.User.Create().SetPassword("fixture").SaveX(ctx)
			owner := context.WithValue(ctx, constant.CtxKeyUser, &user.User{Id: account.ID})
			wallet := client.UserWallet.Create().SetUserID(account.ID).SetBalance(1200).SetGiftAmount(200).SaveX(ctx)
			plan := client.Subscribe.Create().SetName("Monthly").SetSell(true).
				SetInventory(2).SetUnitPrice(1000).SetReplacement(1000).SaveX(ctx)
			client.Payment.Create().SetID(-1).SetName("Balance").SetPlatform("balance").SetConfig("{}").SetEnable(true).SaveX(ctx)
			request := types.V2CreateOrderRequest{Type: orderType, PaymentID: -1}
			if orderType == "purchase" {
				request.SubscribeID = plan.ID
				request.Quantity = 1
			} else {
				subscription := client.UserSubscribe.Create().SetUserID(account.ID).SetOrderID(1).
					SetSubscribeID(plan.ID).SetStatus(1).SaveX(ctx)
				request.UserSubscribeID = subscription.ID
				if orderType == "renewal" {
					request.Quantity = 1
				}
			}
			logic := NewV2OrderLogic(owner, service)
			const key = "balance-payment-replay"
			first, err := logic.CreateAndCheckout(&request, key)
			if err != nil || first == nil || first.Payment == nil || first.Payment.Type != "balance" || first.Payment.PaymentStatus != "paid" || first.Order.PaymentStatus != "paid" {
				t.Fatalf("balance checkout = %+v, %v", first, err)
			}
			replayed, err := logic.CreateAndCheckout(&request, key)
			if err != nil || replayed == nil || replayed.Order != first.Order || replayed.Payment != nil {
				t.Fatalf("replayed checkout = %+v, %v, want original paid order", replayed, err)
			}
			invoice := client.Order.Query().OnlyX(ctx)
			if invoice.PaymentID != -1 || invoice.Method != "balance" || invoice.Status != modelorder.StatusPaid || invoice.Amount != 800 || invoice.GiftAmount != 200 {
				t.Fatalf("persisted order = %+v", invoice)
			}
			if funds := client.UserWallet.GetX(ctx, wallet.ID); funds.Balance != 400 || funds.GiftAmount != 0 {
				t.Fatalf("wallet after replay = %+v, want balance 400 and no gift credit", funds)
			}
			if count := client.OrderEvent.Query().Where(orderevent.OrderIDEQ(invoice.ID), orderevent.EventTypeEQ(modelorder.EventOrderPaymentPaid)).CountX(ctx); count != 1 {
				t.Fatalf("payment events = %d, want one", count)
			}
		})
	}
}
