package portal

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	entorder "github.com/perfect-panel/server/ent/order"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	queue "github.com/perfect-panel/server/queue/types"
)

func TestGuestPurchaseReservesInventoryAndSchedulesOrderClose(t *testing.T) {
	service, db := newCatalogFixture(t)
	ctx := t.Context()
	queueOptions := asynq.RedisClientOpt{Addr: miniredis.RunT(t).Addr()}
	service.Queue = asynq.NewClient(queueOptions)
	inspector := asynq.NewInspector(queueOptions)
	t.Cleanup(func() { _ = service.Queue.Close(); _ = inspector.Close() })
	plan := db.Subscribe.Create().SetName("Guest plan").SetSell(true).SetInventory(1).
		SetUnitPrice(1000).SetDiscount(`[{"quantity":2,"discount":80}]`).SaveX(ctx)
	method := db.Payment.Create().SetPlatform("Stripe").SetConfig(`{}`).SetFeeMode(3).
		SetFeePercent(29).SetFeeAmount(30).SetEnable(true).SaveX(ctx)
	quote, err := NewPrePurchaseOrderLogic(ctx, service).PrePurchaseOrder(&types.PrePurchaseOrderRequest{
		SubscribeId: plan.ID, Quantity: 2, Payment: method.ID,
	})
	if err != nil || quote.FeeAmount != 494 || quote.Amount != 2094 {
		t.Fatalf("guest purchase quote = %+v, err = %v", quote, err)
	}

	logic := NewPurchaseLogic(ctx, service)
	request := types.PortalPurchaseRequest{
		AuthType: "email", Identifier: "Guest@Example.test", Password: "test-password-for-guest",
		SubscribeId: plan.ID, Quantity: 2, Payment: method.ID,
	}
	created, err := logic.Purchase(&request)
	if err != nil || created.OrderNo == "" || created.CheckoutToken == "" {
		t.Fatalf("guest purchase = %+v, err = %v", created, err)
	}
	stored := db.Order.Query().Where(entorder.OrderNo(created.OrderNo)).OnlyX(ctx)
	if stored.Status != order.StatusPending || stored.UserID != 0 || stored.SubscribeID != plan.ID || stored.PaymentID != method.ID ||
		stored.Price != 2000 || stored.Amount != quote.Amount || stored.Discount != 400 || stored.FeeAmount != quote.FeeAmount ||
		stored.GuestIdentifier != "guest@example.test" || stored.GuestCheckoutTokenHash == nil || *stored.GuestCheckoutTokenHash != constant.CheckoutTokenHash(created.CheckoutToken) ||
		stored.GuestPasswordHash == nil || !tool.VerifyPassWord(request.Password, *stored.GuestPasswordHash) {
		t.Fatal("stored order does not match the guest identity and quoted price")
	}
	if plan := db.Subscribe.GetX(ctx, plan.ID); plan.Inventory != 0 || db.User.Query().CountX(ctx) != 0 {
		t.Fatal("purchase did not reserve the last plan or created an account before payment")
	}
	request.Identifier = "second@example.test"
	var code *xerr.CodeError
	if again, err := logic.Purchase(&request); again != nil || !errors.As(err, &code) || code.GetErrCode() != xerr.SubscribeOutOfStock {
		t.Fatalf("second purchase of the last seat = %+v, %v", again, err)
	}
	if count := db.Order.Query().CountX(ctx); count != 1 {
		t.Fatalf("out-of-stock purchase created an order, count = %d", count)
	}
	audit := db.SystemLog.Query().OnlyX(ctx)
	var event log.OrderCreated
	if err := event.Unmarshal([]byte(audit.Content)); err != nil || audit.Type != log.TypeOrderCreated.Uint8() || event.OrderNo != stored.OrderNo || event.Source != "guest" || event.Amount != stored.Amount {
		t.Fatalf("guest order audit event = %+v, %v", event, err)
	}
	tasks, err := inspector.ListScheduledTasks("default")
	if err != nil || len(tasks) != 1 || tasks[0].Type != queue.DeferCloseOrder {
		t.Fatalf("scheduled checkout tasks = %d, %v", len(tasks), err)
	}
	var expiration queue.DeferCloseOrderPayload
	if err := json.Unmarshal(tasks[0].Payload, &expiration); err != nil || expiration.OrderNo != stored.OrderNo {
		t.Fatalf("scheduled checkout expiration = %+v, %v", expiration, err)
	}
}
