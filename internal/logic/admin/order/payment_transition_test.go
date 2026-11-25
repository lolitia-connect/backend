package order

import (
	"database/sql"
	"encoding/json"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	model "github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	queue "github.com/perfect-panel/server/queue/types"
	_ "modernc.org/sqlite"
)

func TestAdminMarkPaidRollsBackUntilActivationIsQueued(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	opt := asynq.RedisClientOpt{Addr: miniredis.RunT(t).Addr()}
	unavailable := asynq.NewClient(opt)
	if err := unavailable.Close(); err != nil {
		t.Fatal(err)
	}
	queueClient, inspector := asynq.NewClient(opt), asynq.NewInspector(opt)
	t.Cleanup(func() { _ = queueClient.Close(); _ = inspector.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil), Queue: unavailable}
	logic := NewUpdateOrderStatusLogic(ctx, service)
	buyer := client.User.Create().SetPassword("fixture").SaveX(ctx)
	payment := client.Payment.Create().SetName("Wire payment").SetPlatform("EPay").SetConfig(`{}`).SaveX(ctx)
	invoice := client.Order.Create().SetOrderNo("invoice-123").SetUserID(buyer.ID).SetType(model.TypeRecharge).
		SetStatus(model.StatusPending).SetPrice(500).SetAmount(500).SetStateVersion(1).SaveX(ctx)
	request := &types.UpdateOrderStatusRequest{Id: invoice.ID, Status: model.StatusPaid, PaymentId: payment.ID, TradeNo: "wire-123"}
	if err := logic.UpdateOrderStatus(request); err == nil {
		t.Fatal("payment acknowledged while the activation queue was unavailable")
	}
	pending := client.Order.GetX(ctx, invoice.ID)
	if pending.Status != model.StatusPending || pending.PaymentID != invoice.PaymentID || pending.TradeNo != nil || pending.StateVersion != 1 || client.OrderEvent.Query().CountX(ctx) != 0 {
		t.Fatal("failed activation enqueue committed a payment or outbox event")
	}
	service.Queue = queueClient
	if err := logic.UpdateOrderStatus(request); err != nil {
		t.Fatal(err)
	}
	if err := logic.UpdateOrderStatus(request); err == nil {
		t.Fatal("second mark-paid request was accepted")
	}
	paid := client.Order.GetX(ctx, invoice.ID)
	if paid.Status != model.StatusPaid || paid.PaymentID != payment.ID || paid.Method != payment.Platform || paid.TradeNo == nil || *paid.TradeNo != request.TradeNo || paid.StateVersion != 2 {
		t.Fatalf("unexpected paid invoice: %+v", paid)
	}
	if n := client.OrderEvent.Query().CountX(ctx); n != 1 {
		t.Fatalf("payment created %d outbox events, want one", n)
	}
	tasks, err := inspector.ListPendingTasks("default")
	if err != nil || len(tasks) != 1 || tasks[0].Type != queue.ForthwithActivateOrder {
		t.Fatalf("activation tasks = %v, %v", tasks, err)
	}
	var activation queue.ForthwithActivateOrderPayload
	if err := json.Unmarshal(tasks[0].Payload, &activation); err != nil || activation.OrderNo != invoice.OrderNo {
		t.Fatalf("activation payload = %+v, %v", activation, err)
	}
	list, err := NewGetOrderListLogic(ctx, service).GetOrderList(&types.GetOrderListRequest{Page: 1, Size: 10, UserId: buyer.ID, Status: model.StatusPaid, Search: "invoice-"})
	if err != nil || list.Total != 1 || len(list.List) != 1 || list.List[0].OrderNo != invoice.OrderNo {
		t.Fatalf("paid invoice list = %+v, %v", list, err)
	}
}
