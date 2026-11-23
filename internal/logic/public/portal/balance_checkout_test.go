package portal

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/ent/orderevent"
	"github.com/perfect-panel/server/ent/systemlog"
	"github.com/perfect-panel/server/ent/userwallet"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	queueTypes "github.com/perfect-panel/server/queue/types"
	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

func TestBalanceCheckoutCommitsOnceAndRollsBackOnAuditFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		amount    int64
		failAudit bool
		wantCode  uint32
	}{
		{name: "gift applied first", amount: 700},
		{name: "insufficient funds", amount: 901, wantCode: xerr.InsufficientBalance},
		{name: "audit insert fails", amount: 700, failAudit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: addr})
			t.Cleanup(func() { _ = cache.Close(); _ = queue.Close(); _ = inspector.Close() })
			service := &svc.ServiceContext{Store: repository.NewStore(client, cache), Redis: cache, Queue: queue}
			account := client.User.Create().SetPassword("fixture").SaveX(ctx)
			client.UserWallet.Create().SetUserID(account.ID).SetBalance(600).SetGiftAmount(300).SetCommission(40).SaveX(ctx)
			method := client.Payment.Create().SetPlatform("balance").SetConfig("{}").SetEnable(true).SaveX(ctx)
			checkout := client.Order.Create().SetOrderNo("balance-checkout").SetUserID(account.ID).SetPaymentID(method.ID).
				SetMethod("balance").SetAmount(tc.amount).SetStatus(order.StatusPending).SetStateVersion(4).SaveX(ctx)
			if tc.failAudit {
				_, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE TRIGGER reject_balance_log BEFORE INSERT ON system_logs WHEN NEW.type = %d BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`, log.TypeBalance.Uint8()))
				if err != nil {
					t.Fatal(err)
				}
			}
			request := &types.CheckoutOrderRequest{OrderNo: checkout.OrderNo}
			response, err := NewPurchaseCheckoutLogic(ctx, service).PurchaseCheckout(request)
			switch {
			case tc.wantCode != 0:
				var code *xerr.CodeError
				if !errors.As(err, &code) || code.GetErrCode() != tc.wantCode || response != nil {
					t.Fatalf("insufficient funds response = %+v, %v", response, err)
				}
			case tc.failAudit:
				if err == nil || !strings.Contains(err.Error(), "audit unavailable") || response != nil {
					t.Fatalf("failed audit response = %+v, %v", response, err)
				}
			default:
				if err != nil || response == nil || response.Type != "balance" {
					t.Fatalf("successful checkout response = %+v, %v", response, err)
				}
				if _, err := NewPurchaseCheckoutLogic(ctx, service).PurchaseCheckout(request); err == nil {
					t.Fatal("duplicate checkout was accepted")
				}
			}
			current := client.Order.GetX(ctx, checkout.ID)
			wallet := client.UserWallet.Query().Where(userwallet.UserIDEQ(account.ID)).OnlyX(ctx)
			events := client.OrderEvent.Query().Where(orderevent.OrderIDEQ(checkout.ID)).AllX(ctx)
			logs := client.SystemLog.Query().Where(systemlog.ObjectIDEQ(account.ID)).AllX(ctx)
			queues, err := inspector.Queues()
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantCode != 0 || tc.failAudit {
				if current.Status != order.StatusPending || current.StateVersion != 4 || current.Amount != tc.amount || current.GiftAmount != 0 || wallet.Balance != 600 || wallet.GiftAmount != 300 || wallet.Commission != 40 || len(events) != 0 || len(logs) != 0 || len(queues) != 0 {
					t.Fatalf("failed checkout changed state: order=%+v wallet=%+v events=%d logs=%d queues=%v", current, wallet, len(events), len(logs), queues)
				}
				return
			}
			if current.Status != order.StatusPaid || current.StateVersion != 5 || current.Amount != 400 || current.GiftAmount != 300 || wallet.Balance != 200 || wallet.GiftAmount != 0 || wallet.Commission != 40 {
				t.Fatalf("wrong payment split or duplicate debit: order=%+v wallet=%+v", current, wallet)
			}
			if len(events) != 1 || events[0].EventType != order.EventOrderPaymentPaid || order.ParseEventPayload(events[0].Payload).StateVersion != 5 {
				t.Fatalf("unexpected durable events: %v", events)
			}
			if len(logs) != 2 {
				t.Fatalf("expected gift and balance audit entries, got %v", logs)
			}
			var gift, balance int
			for _, entry := range logs {
				switch entry.Type {
				case log.TypeGift.Uint8():
					var audit log.Gift
					if err := audit.Unmarshal([]byte(entry.Content)); err != nil || audit.OrderNo != current.OrderNo || audit.Type != log.GiftTypeReduce || audit.Amount != 300 || audit.Balance != 0 {
						t.Fatalf("incorrect gift audit: %+v, %v", audit, err)
					}
					gift++
				case log.TypeBalance.Uint8():
					var audit log.Balance
					if err := audit.Unmarshal([]byte(entry.Content)); err != nil || audit.OrderNo != current.OrderNo || audit.Type != log.BalanceTypePayment || audit.Amount != 400 || audit.Balance != 200 {
						t.Fatalf("incorrect balance audit: %+v, %v", audit, err)
					}
					balance++
				}
			}
			if gift != 1 || balance != 1 {
				t.Fatalf("got %d gift and %d balance audits", gift, balance)
			}
			if len(queues) != 1 {
				t.Fatalf("expected one activation queue, got %v", queues)
			}
			tasks, err := inspector.ListPendingTasks(queues[0])
			if err != nil || len(tasks) != 1 || tasks[0].Type != queueTypes.ForthwithActivateOrder {
				t.Fatalf("unexpected activation tasks: %v, %v", tasks, err)
			}
			var payload queueTypes.ForthwithActivateOrderPayload
			if err := json.Unmarshal(tasks[0].Payload, &payload); err != nil || payload.OrderNo != current.OrderNo {
				t.Fatalf("incorrect activation payload %+v: %v", payload, err)
			}
		})
	}
}
