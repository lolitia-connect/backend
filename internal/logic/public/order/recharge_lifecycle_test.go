package order

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
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
	queuetypes "github.com/perfect-panel/server/queue/types"
	"github.com/stretchr/testify/require"
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
	overflowMethod := client.Payment.Create().SetPlatform("Stripe").SetConfig(`{}`).SetToken("overflow-fee").SetEnable(true).
		SetFeeMode(3).SetFeePercent(2).SetFeeAmount(math.MaxInt64 - 100).SaveX(ctx)
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
		{types.RechargeOrderRequest{Amount: 5000, Payment: overflowMethod.ID + 1}, xerr.PaymentMethodNotFound},
		{types.RechargeOrderRequest{Amount: 5000, Payment: overflowMethod.ID}, xerr.InvalidParams},
		{types.RechargeOrderRequest{Amount: 5100, Payment: overflowMethod.ID}, xerr.InvalidParams},
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

func TestPurchaseRejectsInvalidPlanCouponAndPaymentStates(t *testing.T) {
	tests := []struct {
		name  string
		setup func(context.Context, *svc.ServiceContext, *ent.Client, int64) *types.PurchaseOrderRequest
		code  uint32
	}{
		{
			name: "quantity limit",
			setup: func(_ context.Context, _ *svc.ServiceContext, _ *ent.Client, _ int64) *types.PurchaseOrderRequest {
				return &types.PurchaseOrderRequest{Quantity: MaxQuantity + 1}
			},
			code: xerr.InvalidParams,
		},
		{
			name: "plan is not for sale",
			setup: func(ctx context.Context, _ *svc.ServiceContext, db *ent.Client, _ int64) *types.PurchaseOrderRequest {
				plan := db.Subscribe.Create().SetSell(false).SetInventory(1).SaveX(ctx)
				return &types.PurchaseOrderRequest{SubscribeId: plan.ID}
			},
			code: xerr.SubscribeNotAvailable,
		},
		{
			name: "plan is out of stock",
			setup: func(ctx context.Context, _ *svc.ServiceContext, db *ent.Client, _ int64) *types.PurchaseOrderRequest {
				plan := db.Subscribe.Create().SetSell(true).SetInventory(0).SaveX(ctx)
				return &types.PurchaseOrderRequest{SubscribeId: plan.ID}
			},
			code: xerr.SubscribeOutOfStock,
		},
		{
			name: "coupon is disabled",
			setup: func(ctx context.Context, _ *svc.ServiceContext, db *ent.Client, _ int64) *types.PurchaseOrderRequest {
				plan := db.Subscribe.Create().SetSell(true).SetInventory(1).SetUnitPrice(100).SaveX(ctx)
				coupon := db.Coupon.Create().SetCode("disabled-purchase").SetEnable(false).SetExpireTime(time.Now().Add(time.Hour).UnixMilli()).SaveX(ctx)
				return &types.PurchaseOrderRequest{SubscribeId: plan.ID, Coupon: coupon.Code}
			},
			code: xerr.CouponDisabled,
		},
		{
			name: "coupon is expired",
			setup: func(ctx context.Context, _ *svc.ServiceContext, db *ent.Client, _ int64) *types.PurchaseOrderRequest {
				plan := db.Subscribe.Create().SetSell(true).SetInventory(1).SetUnitPrice(100).SaveX(ctx)
				coupon := db.Coupon.Create().SetCode("expired-purchase").SetExpireTime(time.Now().Add(-time.Hour).UnixMilli()).SaveX(ctx)
				return &types.PurchaseOrderRequest{SubscribeId: plan.ID, Coupon: coupon.Code}
			},
			code: xerr.CouponExpired,
		},
		{
			name: "coupon starts later",
			setup: func(ctx context.Context, _ *svc.ServiceContext, db *ent.Client, _ int64) *types.PurchaseOrderRequest {
				plan := db.Subscribe.Create().SetSell(true).SetInventory(1).SetUnitPrice(100).SaveX(ctx)
				coupon := db.Coupon.Create().SetCode("future-purchase").SetStartTime(time.Now().Add(time.Hour).UnixMilli()).SetExpireTime(time.Now().Add(2 * time.Hour).UnixMilli()).SaveX(ctx)
				return &types.PurchaseOrderRequest{SubscribeId: plan.ID, Coupon: coupon.Code}
			},
			code: xerr.CouponNotApplicable,
		},
		{
			name: "coupon is exhausted",
			setup: func(ctx context.Context, _ *svc.ServiceContext, db *ent.Client, _ int64) *types.PurchaseOrderRequest {
				plan := db.Subscribe.Create().SetSell(true).SetInventory(1).SetUnitPrice(100).SaveX(ctx)
				coupon := db.Coupon.Create().SetCode("used-purchase").SetCount(1).SetUsedCount(1).SetExpireTime(time.Now().Add(time.Hour).UnixMilli()).SaveX(ctx)
				return &types.PurchaseOrderRequest{SubscribeId: plan.ID, Coupon: coupon.Code}
			},
			code: xerr.CouponInsufficientUsage,
		},
		{
			name: "coupon does not match plan",
			setup: func(ctx context.Context, _ *svc.ServiceContext, db *ent.Client, _ int64) *types.PurchaseOrderRequest {
				plan := db.Subscribe.Create().SetSell(true).SetInventory(1).SetUnitPrice(100).SaveX(ctx)
				coupon := db.Coupon.Create().SetCode("other-plan-purchase").SetSubscribe("999").SetExpireTime(time.Now().Add(time.Hour).UnixMilli()).SaveX(ctx)
				return &types.PurchaseOrderRequest{SubscribeId: plan.ID, Coupon: coupon.Code}
			},
			code: xerr.CouponNotApplicable,
		},
		{
			name: "payment method is missing",
			setup: func(ctx context.Context, _ *svc.ServiceContext, db *ent.Client, _ int64) *types.PurchaseOrderRequest {
				plan := db.Subscribe.Create().SetSell(true).SetInventory(1).SetUnitPrice(100).SaveX(ctx)
				return &types.PurchaseOrderRequest{SubscribeId: plan.ID, Payment: 999}
			},
			code: xerr.PaymentMethodNotFound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service, db, _, _ := newPurchaseLifecycleFixture(t)
			ctx := t.Context()
			account := db.User.Create().SetPassword("fixture").SaveX(ctx)
			db.UserWallet.Create().SetUserID(account.ID).SetBalance(1000).SaveX(ctx)
			request := tc.setup(ctx, service, db, account.ID)
			owner := context.WithValue(ctx, constant.CtxKeyUser, &user.User{Id: account.ID})
			_, err := NewPurchaseLogic(owner, service).Purchase(request)
			var appErr *xerr.CodeError
			if !errors.As(err, &appErr) || appErr.GetErrCode() != tc.code {
				t.Fatalf("purchase error = %v, want code %d", err, tc.code)
			}
			if got := db.Order.Query().CountX(ctx); got != 0 {
				t.Fatalf("rejected purchase created %d orders", got)
			}
		})
	}
}

func TestPurchaseHandlesSingleModelAndEmptyWallet(t *testing.T) {
	service, db, _, _ := newPurchaseLifecycleFixture(t)
	ctx := t.Context()
	account := db.User.Create().SetPassword("fixture").SaveX(ctx)
	owner := context.WithValue(ctx, constant.CtxKeyUser, &user.User{Id: account.ID})
	plan := db.Subscribe.Create().SetSell(true).SetInventory(1).SetUnitPrice(100).SaveX(ctx)
	original := db.Order.Create().SetUserID(account.ID).SetOrderNo("single-model-parent").SaveX(ctx)
	db.UserSubscribe.Create().SetUserID(account.ID).SetOrderID(original.ID).SetSubscribeID(plan.ID).SetStartTime(time.Now()).SetExpireTime(time.Now().Add(time.Hour)).SetStatus(1).SaveX(ctx)
	service.Config.Subscribe.SingleModel = true
	_, err := NewPurchaseLogic(owner, service).Purchase(&types.PurchaseOrderRequest{SubscribeId: plan.ID})
	var appErr *xerr.CodeError
	if !errors.As(err, &appErr) || appErr.GetErrCode() != xerr.UserSubscribeExist {
		t.Fatalf("single-model purchase error = %v", err)
	}

	service.Config.Subscribe.SingleModel = false
	method := db.Payment.Create().SetPlatform("balance").SetConfig(`{}`).SetEnable(true).SaveX(ctx)
	response, err := NewPurchaseLogic(owner, service).Purchase(&types.PurchaseOrderRequest{SubscribeId: plan.ID, Payment: method.ID})
	if err != nil || response == nil {
		t.Fatalf("purchase without a wallet = %+v, %v", response, err)
	}
	if got := db.Order.Query().CountX(ctx); got != 2 {
		t.Fatalf("empty-wallet purchase created %d orders, want 2 including the parent", got)
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
			service, client, _, redisOptions := newPurchaseLifecycleFixture(t)
			inspector := asynq.NewInspector(redisOptions)
			t.Cleanup(func() { _ = inspector.Close() })
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
			var voucher *ent.Coupon
			if tc.coupon {
				voucher = client.Coupon.Create().SetCode("RENEW").SetType(2).SetDiscount(100).SetCount(2).
					SetExpireTime(time.Now().Add(time.Hour).UnixMilli()).SaveX(ctx)
			}
			invoice := func() (string, error) {
				if voucher != nil {
					got, err := NewRenewalLogic(owner, service).Renewal(&types.RenewalOrderRequest{UserSubscribeID: sub.ID, Quantity: 2, Payment: method.ID, Coupon: voucher.Code})
					if err != nil {
						return "", err
					}
					return got.OrderNo, nil
				}
				got, err := NewResetTrafficLogic(owner, service).ResetTraffic(&types.ResetTrafficOrderRequest{UserSubscribeID: sub.ID, Payment: method.ID})
				if err != nil {
					return "", err
				}
				return got.OrderNo, nil
			}
			var blockedTable string
			writeError := errors.New("billing storage unavailable")
			client.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
					if m.Type() == blockedTable {
						return nil, writeError
					}
					return next.Mutate(ctx, m)
				})
			})
			stages := []string{"UserWallet", "SystemLog", "Order"}
			if voucher != nil {
				stages = append(stages, "Coupon")
			}
			for _, blockedTable = range stages {
				_, err := invoice()
				require.ErrorIs(t, err, writeError, "failed %s write", blockedTable)
				require.Equal(t, []int64{original.ID}, client.Order.Query().IDsX(ctx))
				require.Zero(t, client.SystemLog.Query().CountX(ctx))
				require.EqualValues(t, 200, client.UserWallet.GetX(ctx, wallet.ID).GiftAmount)
				if voucher != nil {
					require.Zero(t, client.Coupon.GetX(ctx, voucher.ID).UsedCount)
				}
			}
			blockedTable = ""
			number, err := invoice()
			require.NoError(t, err)
			if voucher != nil {
				require.EqualValues(t, 1, client.Coupon.GetX(ctx, voucher.ID).UsedCount)
			}
			jobs, err := inspector.ListScheduledTasks("default")
			require.NoError(t, err)
			require.Len(t, jobs, 1)
			require.Equal(t, queuetypes.DeferCloseOrder, jobs[0].Type)
			var payload queuetypes.DeferCloseOrderPayload
			require.NoError(t, json.Unmarshal(jobs[0].Payload, &payload))
			require.Equal(t, number, payload.OrderNo)
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
