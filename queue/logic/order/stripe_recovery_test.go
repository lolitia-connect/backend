package orderLogic

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/ent/orderevent"
	"github.com/perfect-panel/server/internal/logic/notify"
	publicorder "github.com/perfect-panel/server/internal/logic/public/order"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/queue/types"
	"github.com/redis/go-redis/v9"
	stripeapi "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/webhook"
	_ "modernc.org/sqlite"
)

func stripeTestService(t *testing.T) (*svc.ServiceContext, *asynq.Inspector) {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	addr := miniredis.RunT(t).Addr()
	cache := redis.NewClient(&redis.Options{Addr: addr})
	opt := asynq.RedisClientOpt{Addr: addr, DB: 5}
	queue, inspector := asynq.NewClient(opt), asynq.NewInspector(opt)
	t.Cleanup(func() { _ = cache.Close(); _ = queue.Close(); _ = inspector.Close() })
	service := &svc.ServiceContext{Ent: client, Store: repository.NewStore(client, cache), Redis: cache, Queue: queue}
	service.Config.JwtAuth.AccessSecret = "return-test"
	return service, inspector
}

func stripeLookupServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	original := stripeapi.GetBackend(stripeapi.APIBackend)
	stripeapi.SetBackend(stripeapi.APIBackend, stripeapi.GetBackendWithConfig(stripeapi.APIBackend, &stripeapi.BackendConfig{URL: stripeapi.String(server.URL), HTTPClient: server.Client(), MaxNetworkRetries: stripeapi.Int64(0)}))
	t.Cleanup(func() { stripeapi.SetBackend(stripeapi.APIBackend, original); server.Close() })
}

func deliverStripeEvent(t *testing.T, service *svc.ServiceContext, method *payment.Payment, eventType string, session map[string]any) error {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"type": eventType, "api_version": stripeapi.APIVersion, "data": map[string]any{"object": session}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	req := httptest.NewRequest(http.MethodPost, "/notify", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", fmt.Sprintf("t=%d,v1=%x", now.Unix(), webhook.ComputeSignature(now, payload, "test-signature")))
	ctx := context.WithValue(t.Context(), constant.CtxKeyPayment, method)
	return notify.NewStripeNotifyLogic(ctx, service).StripeNotify(req, httptest.NewRecorder())
}

func TestStripeReturnWebhookAndRecoveryCreditOnce(t *testing.T) {
	for _, first := range []string{"return", "webhook", "overlap", "scanner"} {
		t.Run(first, func(t *testing.T) {
			service, inspector := stripeTestService(t)
			ctx, db := t.Context(), service.Ent
			buyer := db.User.Create().SetPassword("fixture").SaveX(ctx)
			wallet := db.UserWallet.Create().SetUserID(buyer.ID).SetBalance(200).SaveX(ctx)
			row := db.Payment.Create().SetPlatform("Stripe").SetConfig(`{"secret_key":"lookup-test","webhook_secret":"test-signature"}`).SaveX(ctx)
			method, err := service.Store.Payment().FindOne(ctx, row.ID)
			if err != nil {
				t.Fatal(err)
			}
			current := db.Order.Create().SetOrderNo("recharge").SetUserID(buyer.ID).SetPaymentID(row.ID).SetMethod("Stripe").SetType(order.TypeRecharge).SetPrice(1000).SetAmount(1030).SetPaymentAmount(1030).SetPaymentCurrency("USD").SetTradeNo("cs_return").SetStateVersion(1).SetCreatedAt(time.Now().Add(-time.Hour)).SaveX(ctx)
			session := map[string]any{"id": "cs_return", "client_reference_id": current.OrderNo, "status": "complete", "payment_status": "paid", "amount_total": 1030, "currency": "usd"}
			var queries atomic.Int32
			stripeLookupServer(t, func(w http.ResponseWriter, r *http.Request) {
				queries.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/v1/checkout/sessions/cs_return" {
					t.Errorf("unexpected gateway request %s %s", r.Method, r.URL.Path)
				}
				if first == "overlap" {
					if err := deliverStripeEvent(t, service, method, "checkout.session.completed", session); err != nil {
						t.Error(err)
					}
				}
				_ = json.NewEncoder(w).Encode(session)
			})
			logic := publicorder.NewV2OrderLogic(context.WithValue(ctx, constant.CtxKeyUser, &user.User{Id: buyer.ID}), service)
			confirm := func() {
				t.Helper()
				result, err := logic.Reconcile(current.OrderNo, "")
				if err != nil || result.Order.PaymentStatus != "paid" {
					t.Fatalf("return = %+v, %v", result, err)
				}
			}
			deliver := func() error { return deliverStripeEvent(t, service, method, "checkout.session.completed", session) }
			switch first {
			case "return", "overlap":
				confirm()
			case "webhook":
				working := service.Queue
				broken := asynq.NewClient(asynq.RedisClientOpt{Addr: service.Redis.Options().Addr, DB: 5})
				_ = broken.Close()
				service.Queue = broken
				if err := deliver(); err == nil {
					t.Fatal("lost activation enqueue was acknowledged")
				}
				if db.Order.GetX(ctx, current.ID).Status != order.StatusPaid {
					t.Fatal("payment was lost with the queue")
				}
				service.Queue = working
				db.Order.UpdateOneID(current.ID).SetUpdatedAt(time.Now().Add(-2 * time.Minute)).ExecX(ctx)
				if err := RecoverStripeOrders(service)(ctx, nil); err != nil {
					t.Fatal(err)
				}
			case "scanner":
				if err := RecoverStripeOrders(service)(ctx, nil); err != nil {
					t.Fatal(err)
				}
				tasks, err := inspector.ListPendingTasks("default")
				if err != nil || len(tasks) != 1 || tasks[0].Type != types.DeferCloseOrder {
					t.Fatalf("missing gateway check: %v, %v", tasks, err)
				}
				if err := NewDeferCloseOrderLogic(service).ProcessTask(ctx, asynq.NewTask(tasks[0].Type, tasks[0].Payload)); err != nil {
					t.Fatal(err)
				}
			}
			if db.UserWallet.GetX(ctx, wallet.ID).Balance != 200 {
				t.Fatal("payment confirmation credited before fulfillment")
			}
			tasks, err := inspector.ListPendingTasks("default")
			if err != nil {
				t.Fatal(err)
			}
			var activation *asynq.Task
			for _, item := range tasks {
				if item.Type == types.ForthwithActivateOrder {
					if activation != nil {
						t.Fatal("duplicate activation")
					}
					activation = asynq.NewTask(item.Type, item.Payload)
				}
			}
			if activation == nil {
				t.Fatal("activation was not recovered")
			}
			confirm()
			if err := deliver(); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := NewActivateOrderLogic(service).ProcessTask(ctx, activation); err != nil {
					t.Fatal(err)
				}
			}
			if err := deliver(); err != nil {
				t.Fatal(err)
			}
			got := db.Order.GetX(ctx, current.ID)
			if got.Status != order.StatusFinished || got.StateVersion != 3 || db.UserWallet.GetX(ctx, wallet.ID).Balance != 1200 || db.OrderEvent.Query().Where(orderevent.OrderID(current.ID)).CountX(ctx) != 2 {
				t.Fatal("expected one payment, fulfillment and credit")
			}
			if (first == "webhook" && queries.Load() != 0) || (first != "webhook" && queries.Load() != 1) {
				t.Fatalf("gateway queries = %d", queries.Load())
			}
		})
	}
}

func TestStripeReturnRequiresMatchingPaidSessionAndGuestCapability(t *testing.T) {
	service, _ := stripeTestService(t)
	ctx := t.Context()
	method := service.Ent.Payment.Create().SetPlatform("Stripe").SetConfig(`{"secret_key":"lookup-test"}`).SaveX(ctx)
	for _, change := range []string{"unpaid", "order", "trade", "amount", "currency", "valid"} {
		t.Run(change, func(t *testing.T) {
			row := service.Ent.Order.Create().SetOrderNo(change).SetMethod("Stripe").SetTradeNo("cs_" + change).SetPaymentID(method.ID).SetPaymentAmount(100).SetPaymentCurrency("USD").SetGuestCheckoutTokenHash(constant.CheckoutTokenHash("capability")).SaveX(ctx)
			receipt := map[string]any{"id": *row.TradeNo, "client_reference_id": row.OrderNo, "payment_status": "paid", "amount_total": 100, "currency": "usd"}
			switch change {
			case "unpaid":
				receipt["payment_status"] = "unpaid"
			case "order":
				receipt["client_reference_id"] = "other"
			case "trade":
				receipt["id"] = "cs_other"
			case "amount":
				receipt["amount_total"] = 99
			case "currency":
				receipt["currency"] = "eur"
			}
			var lookups atomic.Int32
			stripeLookupServer(t, func(w http.ResponseWriter, _ *http.Request) { lookups.Add(1); _ = json.NewEncoder(w).Encode(receipt) })
			logic := publicorder.NewV2OrderLogic(ctx, service)
			if _, err := logic.Reconcile(row.OrderNo, "wrong"); err == nil || lookups.Load() != 0 {
				t.Fatal("unauthorized order lookup")
			}
			for range 2 {
				result, err := logic.Reconcile(row.OrderNo, "capability")
				if err != nil || (result.Order.PaymentStatus == "paid") != (change == "valid") {
					t.Fatalf("reconcile = %+v, %v", result, err)
				}
			}
			if lookups.Load() != 1 {
				t.Fatalf("gateway queries = %d", lookups.Load())
			}
		})
	}
}

func TestStripeAsyncFailureRechecksBeforeReleasingOrder(t *testing.T) {
	service, inspector := stripeTestService(t)
	ctx, db := t.Context(), service.Ent
	method := db.Payment.Create().SetPlatform("Stripe").SetConfig(`{"secret_key":"test","webhook_secret":"test-signature"}`).SaveX(ctx)
	config, err := service.Store.Payment().FindOne(ctx, method.ID)
	if err != nil {
		t.Fatal(err)
	}
	row := db.Order.Create().SetOrderNo("delayed").SetMethod("Stripe").SetPaymentID(method.ID).SetType(order.TypeRecharge).SetTradeNo("cs_delayed").SetPaymentAmount(100).SetPaymentCurrency("USD").SaveX(ctx)
	receipt := map[string]any{"id": "cs_delayed", "client_reference_id": row.OrderNo, "payment_status": "unpaid", "status": "complete", "amount_total": 100, "currency": "usd"}
	if err := deliverStripeEvent(t, service, config, "checkout.session.completed", receipt); err != nil {
		t.Fatal(err)
	}
	if err := deliverStripeEvent(t, service, config, "checkout.session.async_payment_failed", receipt); err != nil {
		t.Fatal(err)
	}
	tasks, err := inspector.ListPendingTasks("default")
	if err != nil || len(tasks) != 1 || tasks[0].Type != types.DeferCloseOrder {
		t.Fatalf("expected a remote check, got %v, %v", tasks, err)
	}
	var failed atomic.Bool
	stripeLookupServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("attempted to cancel a completed Checkout")
		}
		intent := "processing"
		if failed.Load() {
			intent = "requires_payment_method"
		}
		session := map[string]any{}
		for k, v := range receipt {
			session[k] = v
		}
		session["payment_intent"] = map[string]any{"id": "pi_delayed", "status": intent}
		_ = json.NewEncoder(w).Encode(session)
	})
	check := asynq.NewTask(tasks[0].Type, tasks[0].Payload)
	if err := NewDeferCloseOrderLogic(service).ProcessTask(ctx, check); err != nil || db.Order.GetX(ctx, row.ID).Status != order.StatusPending {
		t.Fatalf("processing payment lost reservation: %v", err)
	}
	failed.Store(true)
	if err := NewDeferCloseOrderLogic(service).ProcessTask(ctx, check); err != nil || db.Order.GetX(ctx, row.ID).Status != order.StatusClosed {
		t.Fatalf("definitively failed payment was not closed: %v", err)
	}
}

func TestGuestRegistrationSurvivesLostCacheAndReadsLegacyOrders(t *testing.T) {
	service, _ := stripeTestService(t)
	ctx := t.Context()
	for _, source := range []string{"database", "legacy-cache"} {
		identifier := source + "@example.test"
		current := &order.Order{OrderNo: source, Status: order.StatusPaid, GuestAuthType: "email", GuestIdentifier: identifier}
		if source == "database" {
			current.GuestPasswordHash = tool.EncodePassWord("guest password")
		} else {
			legacy := constant.TemporaryOrderInfo{OrderNo: source, AuthType: "email", Identifier: identifier, Password: "guest password"}
			payload, err := legacy.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			if err := service.Redis.Set(ctx, fmt.Sprintf(constant.TempOrderCacheKey, source), payload, time.Hour).Err(); err != nil {
				t.Fatal(err)
			}
		}
		if err := service.Store.Order().Insert(ctx, current); err != nil {
			t.Fatal(err)
		}
		stored, err := service.Store.Order().FindOneByOrderNo(ctx, source)
		if err != nil {
			t.Fatal(err)
		}
		account, err := NewActivateOrderLogic(service).createGuestUser(ctx, stored)
		if err != nil {
			t.Fatal(err)
		}
		if !tool.VerifyPassWord("guest password", account.Password) {
			t.Fatal("stored registration did not produce the correct account credentials")
		}
		ok, err := service.Store.Order().FinishOrder(ctx, source, false)
		if !ok || err != nil {
			t.Fatalf("finish = %t, %v", ok, err)
		}
		if service.Ent.Order.GetX(ctx, stored.Id).GuestPasswordHash != nil {
			t.Fatal("finished order retained guest password hash")
		}
	}
}
