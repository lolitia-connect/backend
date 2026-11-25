package order

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/ent/orderevent"
	"github.com/perfect-panel/server/ent/systemlog"
	"github.com/perfect-panel/server/internal/model/log"
	modelorder "github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"
	queuetypes "github.com/perfect-panel/server/queue/types"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func newPurchaseLifecycleFixture(t *testing.T) (*svc.ServiceContext, *ent.Client, *sql.DB, asynq.RedisClientOpt) {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	options := asynq.RedisClientOpt{Addr: miniredis.RunT(t).Addr()}
	cache := redis.NewClient(&redis.Options{Addr: options.Addr})
	queue := asynq.NewClient(options)
	t.Cleanup(func() { _ = cache.Close(); _ = queue.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, cache), Redis: cache, Queue: queue}
	service.Config.JwtAuth.AccessSecret = "purchase-lifecycle-test-secret"
	return service, client, db, options
}

func TestPurchasePreviewAppliesDiscountsAndFeesWithoutReservingFunds(t *testing.T) {
	service, client, _, _ := newPurchaseLifecycleFixture(t)
	ctx := t.Context()
	account := client.User.Create().SetPassword("fixture").SaveX(ctx)
	owner := context.WithValue(ctx, constant.CtxKeyUser, &user.User{Id: account.ID})
	wallet := client.UserWallet.Create().SetUserID(account.ID).SetBalance(1200).SetGiftAmount(200).SaveX(ctx)
	plan := client.Subscribe.Create().SetName("Regional").SetSell(true).SetQuota(1).SetInventory(2).SetUnitPrice(1000).
		SetDiscount(`[{"quantity":2,"discount":80}]`).SaveX(ctx)
	voucher := client.Coupon.Create().SetCode("PREVIEW").SetType(1).SetDiscount(25).SetCount(1).
		SetExpireTime(time.Now().Add(time.Hour).UnixMilli()).SaveX(ctx)
	method := client.Payment.Create().SetPlatform("Stripe").SetConfig(`{}`).SetFeeMode(3).SetFeePercent(5).SetFeeAmount(30).SaveX(ctx)
	logic := NewPreCreateOrderLogic(owner, service)
	req := types.PurchaseOrderRequest{SubscribeId: plan.ID, Quantity: 2, Payment: method.ID, Coupon: voucher.Code}
	preview, err := logic.PreCreateOrder(&req)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Price != 2000 || preview.Discount != 400 || preview.CouponDiscount != 400 || preview.FeeAmount != 80 || preview.GiftAmount != 200 || preview.Amount != 1080 {
		t.Fatalf("unexpected checkout quote: %+v", preview)
	}
	if got := client.UserWallet.GetX(ctx, wallet.ID); got.Balance != 1200 || got.GiftAmount != 200 {
		t.Fatalf("quote changed wallet funds: %+v", got)
	}
	if client.Order.Query().CountX(ctx) != 0 || client.Subscribe.GetX(ctx, plan.ID).Inventory != 2 || client.Coupon.GetX(ctx, voucher.ID).UsedCount != 0 {
		t.Fatal("quote reserved inventory or a coupon, or created an order")
	}
	invalid := req
	invalid.Coupon = "NOT-FOUND"
	var code *xerr.CodeError
	if got, err := logic.PreCreateOrder(&invalid); got != nil || !errors.As(err, &code) || code.GetErrCode() != xerr.CouponNotExist {
		t.Fatalf("unknown coupon returned %+v, %v", got, err)
	}
	client.UserSubscribe.Create().SetUserID(account.ID).SetOrderID(1).SetSubscribeID(plan.ID).SaveX(ctx)
	if got, err := logic.PreCreateOrder(&req); got != nil || !errors.As(err, &code) || code.GetErrCode() != xerr.SubscribeQuotaLimit {
		t.Fatalf("quote beyond plan quota returned %+v, %v", got, err)
	}
}

func TestV2PurchaseReplayDoesNotRepeatReservationsOrBalancePayment(t *testing.T) {
	service, client, _, queueOptions := newPurchaseLifecycleFixture(t)
	ctx := t.Context()
	account := client.User.Create().SetPassword("fixture").SaveX(ctx)
	owner := context.WithValue(ctx, constant.CtxKeyUser, &user.User{Id: account.ID})
	wallet := client.UserWallet.Create().SetUserID(account.ID).SetBalance(1200).SetGiftAmount(200).SaveX(ctx)
	plan := client.Subscribe.Create().SetName("Monthly").SetSell(true).SetInventory(2).SetUnitPrice(1000).SaveX(ctx)
	coupon := client.Coupon.Create().SetCode("V2-REPLAY").SetType(2).SetDiscount(100).SetCount(2).
		SetExpireTime(time.Now().Add(time.Hour).UnixMilli()).SaveX(ctx)
	method := client.Payment.Create().SetPlatform("balance").SetConfig(`{}`).SetEnable(true).SaveX(ctx)

	logic := NewV2OrderLogic(owner, service)
	request := types.V2CreateOrderRequest{Type: "purchase", PaymentID: method.ID, SubscribeID: plan.ID, Quantity: 1, Coupon: coupon.Code}
	const key = "paid-purchase-replay"
	first, err := logic.CreateAndCheckout(&request, key)
	if err != nil || first == nil || first.Order.OrderNo == "" || first.Order.PaymentStatus != "paid" || first.Payment == nil || first.Payment.Type != "balance" || first.Payment.PaymentStatus != "paid" {
		t.Fatalf("first checkout = %+v, %v", first, err)
	}
	replayed, err := logic.CreateAndCheckout(&request, key)
	if err != nil || replayed == nil || replayed.Order != first.Order || replayed.Payment != nil {
		t.Fatalf("replayed checkout = %+v, %v, want original paid snapshot", replayed, err)
	}
	request.Quantity = 2
	if conflicting, err := logic.CreateAndCheckout(&request, key); !errors.Is(err, ErrIdempotencyKeyReused) || conflicting != nil {
		t.Fatalf("changed request using the same key = %+v, %v", conflicting, err)
	}

	invoice := client.Order.Query().OnlyX(ctx)
	if invoice.OrderNo != first.Order.OrderNo || invoice.UserID != account.ID || invoice.SubscribeID != plan.ID || invoice.Status != modelorder.StatusPaid ||
		invoice.IdempotencyKey == nil || *invoice.IdempotencyKey != key || invoice.Quantity != 1 || invoice.Amount != 700 || invoice.GiftAmount != 200 || !invoice.CouponReserved || invoice.CouponDiscount != 100 {
		t.Fatalf("persisted purchase = %+v", invoice)
	}
	if got := client.UserWallet.GetX(ctx, wallet.ID); got.Balance != 500 || got.GiftAmount != 0 {
		t.Fatalf("wallet after all requests = %+v", got)
	}
	if stock, uses := client.Subscribe.GetX(ctx, plan.ID).Inventory, client.Coupon.GetX(ctx, coupon.ID).UsedCount; stock != 1 || uses != 1 {
		t.Fatalf("stock = %d, coupon uses = %d, want one reservation of each", stock, uses)
	}
	events := client.OrderEvent.Query().Where(orderevent.OrderIDEQ(invoice.ID)).Order(ent.Asc(orderevent.FieldID)).AllX(ctx)
	if len(events) != 2 || events[0].EventType != modelorder.EventOrderCreated || events[1].EventType != modelorder.EventOrderPaymentPaid ||
		modelorder.ParseEventPayload(events[1].Payload).StateVersion != invoice.StateVersion {
		t.Fatalf("persisted events = %+v, want one creation and one payment at version %d", events, invoice.StateVersion)
	}
	inspector := asynq.NewInspector(queueOptions)
	t.Cleanup(func() { _ = inspector.Close() })
	tasks, err := inspector.ListPendingTasks("default")
	if err != nil || len(tasks) != 1 || tasks[0].Type != queuetypes.ForthwithActivateOrder {
		t.Fatalf("pending tasks = %+v, %v, want one activation", tasks, err)
	}
}

func TestCancelPurchaseRollsBackAndReleasesReservationsOnce(t *testing.T) {
	service, client, db, _ := newPurchaseLifecycleFixture(t)
	ctx := t.Context()
	account := client.User.Create().SetPassword("fixture").SaveX(ctx)
	owner := context.WithValue(ctx, constant.CtxKeyUser, &user.User{Id: account.ID})
	wallet := client.UserWallet.Create().SetUserID(account.ID).SetBalance(450).SetGiftAmount(250).SaveX(ctx)
	plan := client.Subscribe.Create().SetName("Last seat").SetSell(true).SetInventory(1).SetUnitPrice(1000).SaveX(ctx)
	coupon := client.Coupon.Create().SetCode("CANCEL").SetType(2).SetDiscount(100).SetCount(1).
		SetExpireTime(time.Now().Add(time.Hour).UnixMilli()).SaveX(ctx)
	method := client.Payment.Create().SetPlatform("Stripe").SetConfig(`{}`).SetFeeMode(2).SetFeeAmount(30).SetEnable(true).SaveX(ctx)
	created, err := NewPurchaseLogic(owner, service).Purchase(&types.PurchaseOrderRequest{
		SubscribeId: plan.ID, Quantity: 1, Payment: method.ID, Coupon: coupon.Code,
	})
	if err != nil || created == nil {
		t.Fatalf("create purchase = %+v, %v", created, err)
	}
	invoice := client.Order.Query().OnlyX(ctx)
	if invoice.OrderNo != created.OrderNo || invoice.Amount != 680 || invoice.FeeAmount != 30 || invoice.GiftAmount != 250 {
		t.Fatalf("persisted purchase = %+v", invoice)
	}

	type purchaseState struct {
		status                              uint8
		couponReserved                      bool
		version, gift, balance, stock, uses int64
		events, giftLogs                    int
	}
	assertState := func(want purchaseState) {
		t.Helper()
		current := client.Order.GetX(ctx, invoice.ID)
		funds := client.UserWallet.GetX(ctx, wallet.ID)
		got := purchaseState{
			status: current.Status, couponReserved: current.CouponReserved, version: current.StateVersion,
			gift: funds.GiftAmount, balance: funds.Balance,
			stock:    client.Subscribe.GetX(ctx, plan.ID).Inventory,
			uses:     client.Coupon.GetX(ctx, coupon.ID).UsedCount,
			events:   client.OrderEvent.Query().Where(orderevent.OrderIDEQ(invoice.ID)).CountX(ctx),
			giftLogs: client.SystemLog.Query().Where(systemlog.ObjectIDEQ(account.ID), systemlog.TypeEQ(log.TypeGift.Uint8())).CountX(ctx),
		}
		if got != want {
			t.Fatalf("persisted purchase state = %+v, want %+v", got, want)
		}
	}
	pending := purchaseState{
		status: modelorder.StatusPending, couponReserved: true, version: invoice.StateVersion,
		balance: 450, uses: 1, events: 1, giftLogs: 1,
	}
	assertState(pending)

	// Fail the stock update after close, coupon release, and refund have been written in the transaction.
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_inventory_restore BEFORE UPDATE OF inventory ON subscribe
		WHEN NEW.inventory > OLD.inventory BEGIN SELECT RAISE(ABORT, 'inventory restore unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	request := &types.CloseOrderRequest{OrderNo: invoice.OrderNo}
	if err := NewCloseOrderLogic(ctx, service).CloseOrder(request); err == nil || !strings.Contains(err.Error(), "inventory restore unavailable") {
		t.Fatalf("close with failing stock update = %v", err)
	}
	assertState(pending)

	if _, err := db.ExecContext(ctx, `DROP TRIGGER reject_inventory_restore`); err != nil {
		t.Fatal(err)
	}
	cancelled := purchaseState{
		status: modelorder.StatusClosed, version: invoice.StateVersion + 1,
		gift: 250, balance: 450, stock: 1, events: 2, giftLogs: 2,
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := NewCloseOrderLogic(ctx, service).CloseOrder(request); err != nil {
			t.Fatalf("close attempt %d = %v", attempt+1, err)
		}
		assertState(cancelled)
	}
	closed := client.OrderEvent.Query().Where(orderevent.OrderIDEQ(invoice.ID), orderevent.EventTypeEQ(modelorder.EventOrderClosed)).OnlyX(ctx)
	if payload := modelorder.ParseEventPayload(closed.Payload); payload.OrderNo != invoice.OrderNo || payload.PaymentStatus != "closed" || payload.StateVersion != invoice.StateVersion+1 {
		t.Fatalf("close event = %+v", payload)
	}
}
