package order

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	entorder "github.com/perfect-panel/server/ent/order"
	"github.com/perfect-panel/server/internal/model/log"
	modelorder "github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"
	_ "modernc.org/sqlite"
)

func newCustomerOrderFixture(t *testing.T) (*svc.ServiceContext, *ent.Client) {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	queue := asynq.NewClient(asynq.RedisClientOpt{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = queue.Close() })
	return &svc.ServiceContext{Store: repository.NewStore(client, nil), Queue: queue}, client
}

func TestRechargePersistsPendingInvoiceAndAppearsInCustomerHistory(t *testing.T) {
	service, client := newCustomerOrderFixture(t)
	ctx := t.Context()
	account := client.User.Create().SetPassword("fixture").SaveX(ctx)
	userCtx := context.WithValue(ctx, constant.CtxKeyUser, &user.User{Id: account.ID})
	wallet := client.UserWallet.Create().SetUserID(account.ID).SetBalance(400).SaveX(ctx)
	method := client.Payment.Create().SetName("Card").SetPlatform("Stripe").SetConfig(`{}`).
		SetEnable(true).SetFeeMode(3).SetFeePercent(2).SetFeeAmount(30).SaveX(ctx)
	first := client.Order.Create().SetOrderNo("previous-order").SetUserID(account.ID).SetPrice(1000).
		SetAmount(1000).SetCommission(50).SetStatus(modelorder.StatusFinished).SaveX(ctx)
	other := client.User.Create().SetPassword("fixture").SaveX(ctx)
	client.Order.Create().SetOrderNo("another-customer").SetUserID(other.ID).SetAmount(2000).SaveX(ctx)

	logic := NewRechargeLogic(userCtx, service)
	for _, tc := range []struct {
		req  types.RechargeOrderRequest
		code uint32
	}{
		{types.RechargeOrderRequest{Amount: 0, Payment: method.ID}, xerr.InvalidParams},
		{types.RechargeOrderRequest{Amount: MaxRechargeAmount + 1, Payment: method.ID}, xerr.InvalidParams},
		{types.RechargeOrderRequest{Amount: 5000, Payment: method.ID + 1}, xerr.PaymentMethodNotFound},
	} {
		resp, err := logic.Recharge(&tc.req)
		var code *xerr.CodeError
		if resp != nil || !errors.As(err, &code) || code.GetErrCode() != tc.code {
			t.Fatalf("recharge %+v = %+v, %v", tc.req, resp, err)
		}
	}
	if client.Order.Query().CountX(ctx) != 2 || client.SystemLog.Query().CountX(ctx) != 0 {
		t.Fatal("invalid recharge created an order or audit event")
	}

	created, err := logic.Recharge(&types.RechargeOrderRequest{Amount: 5000, Payment: method.ID})
	if err != nil || created.OrderNo == "" {
		t.Fatalf("create recharge = %+v, %v", created, err)
	}
	stored := client.Order.Query().Where(entorder.OrderNo(created.OrderNo)).OnlyX(ctx)
	if stored.UserID != account.ID || stored.Status != modelorder.StatusPending || stored.Type != modelorder.TypeRecharge || stored.Price != 5000 || stored.Amount != 5130 || stored.FeeAmount != 130 {
		t.Fatalf("stored recharge = %+v", stored)
	}
	if got := client.UserWallet.GetX(ctx, wallet.ID); got.Balance != 400 {
		t.Fatalf("pending recharge credited wallet prematurely: %d", got.Balance)
	}
	audit := client.SystemLog.Query().OnlyX(ctx)
	var event log.OrderCreated
	if err := event.Unmarshal([]byte(audit.Content)); err != nil || audit.ObjectID != account.ID || audit.Type != log.TypeOrderCreated.Uint8() || event.OrderNo != created.OrderNo || event.Amount != 5130 {
		t.Fatalf("recharge audit = %+v, %v", event, err)
	}

	list := NewQueryOrderListLogic(userCtx, service)
	for index, want := range []string{created.OrderNo, first.OrderNo} {
		page, err := list.QueryOrderList(&types.QueryOrderListRequest{Page: index + 1, Size: 1})
		if err != nil || page.Total != 2 || len(page.List) != 1 || page.List[0].OrderNo != want || page.List[0].Commission != 0 {
			t.Fatalf("history page %d = %+v, %v", index+1, page, err)
		}
	}
	detail, err := NewQueryOrderDetailLogic(userCtx, service).QueryOrderDetail(&types.QueryOrderDetailRequest{OrderNo: created.OrderNo})
	if err != nil || detail.Id != stored.ID || detail.Amount != 5130 || detail.Payment.Id != method.ID || detail.Payment.Name != "Card" {
		t.Fatalf("recharge detail = %+v, %v", detail, err)
	}
}

func TestRenewalAndResetDoNotChangeSubscriptionUntilPayment(t *testing.T) {
	for _, tc := range []struct {
		name       string
		kind       uint8
		price, due int64
		coupon     bool
	}{
		{"renewal", modelorder.TypeRenewal, 2000, 1330, true},
		{"traffic reset", modelorder.TypeResetTraffic, 700, 530, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, client := newCustomerOrderFixture(t)
			ctx := t.Context()
			account := client.User.Create().SetPassword("fixture").SaveX(ctx)
			owner := context.WithValue(ctx, constant.CtxKeyUser, &user.User{Id: account.ID})
			wallet := client.UserWallet.Create().SetUserID(account.ID).SetGiftAmount(200).SetBalance(50).SaveX(ctx)
			plan := client.Subscribe.Create().SetName("Starter").SetSell(true).SetInventory(4).SetUnitPrice(1000).SetReplacement(700).
				SetDiscount(`[{"quantity":2,"discount":80}]`).SaveX(ctx)
			original := client.Order.Create().SetUserID(account.ID).SetOrderNo("initial-payment").SetSubscribeID(plan.ID).SetStatus(modelorder.StatusFinished).SaveX(ctx)
			sub := client.UserSubscribe.Create().SetUserID(account.ID).SetOrderID(original.ID).SetSubscribeID(plan.ID).
				SetStatus(1).SetExpireTime(time.Now().Add(24 * time.Hour)).SetDownload(1234).SetUpload(123).SaveX(ctx)
			method := client.Payment.Create().SetPlatform("Stripe").SetConfig(`{}`).SetFeeMode(2).SetFeeAmount(30).SaveX(ctx)
			var number string
			if tc.coupon {
				voucher := client.Coupon.Create().SetCode("RENEW").SetType(2).SetDiscount(100).SetCount(2).
					SetExpireTime(time.Now().Add(time.Hour).UnixMilli()).SaveX(ctx)
				got, err := NewRenewalLogic(owner, service).Renewal(&types.RenewalOrderRequest{UserSubscribeID: sub.ID, Quantity: 2, Payment: method.ID, Coupon: voucher.Code})
				if err != nil {
					t.Fatal(err)
				}
				number = got.OrderNo
				if stored := client.Coupon.GetX(ctx, voucher.ID); stored.UsedCount != 1 {
					t.Fatalf("renewal coupon uses = %d, want 1", stored.UsedCount)
				}
			} else {
				got, err := NewResetTrafficLogic(owner, service).ResetTraffic(&types.ResetTrafficOrderRequest{UserSubscribeID: sub.ID, Payment: method.ID})
				if err != nil {
					t.Fatal(err)
				}
				number = got.OrderNo
			}
			inv := client.Order.Query().Where(entorder.OrderNo(number)).OnlyX(ctx)
			if inv.Type != tc.kind || inv.Status != modelorder.StatusPending || inv.ParentID != original.ID || inv.UserSubscribeID != sub.ID || inv.Price != tc.price || inv.Amount != tc.due || inv.GiftAmount != 200 || inv.FeeAmount != 30 {
				t.Fatalf("pending subscription invoice = %+v", inv)
			}
			if current := client.UserSubscribe.GetX(ctx, sub.ID); !current.ExpireTime.Equal(sub.ExpireTime) || current.Download != sub.Download || current.Upload != sub.Upload {
				t.Fatalf("pending invoice changed subscription: %+v", current)
			}
			if updated := client.UserWallet.GetX(ctx, wallet.ID); updated.GiftAmount != 0 || updated.Balance != 50 {
				t.Fatalf("gift reservation changed wallet incorrectly: %+v", updated)
			}
			if remaining := client.Subscribe.GetX(ctx, plan.ID).Inventory; remaining != 4 {
				t.Fatalf("subscription inventory = %d, want 4", remaining)
			}
		})
	}
}
