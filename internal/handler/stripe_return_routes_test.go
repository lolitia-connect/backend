package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	worker "github.com/perfect-panel/server/queue/logic/order"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/stretchr/testify/require"
	stripe "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/webhook"
)

func TestStripeMethodPublicationAndWebhookRotation(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	t.Setenv("GATEWAY_MODE", "false")
	_, operator := f.account(true)
	buyer, session := f.account(false)
	f.client.UserWallet.Create().SetUserID(buyer.ID).SaveX(ctx)
	plan := f.client.Subscribe.Create().SetName("Monthly").SetSell(true).SetUnitPrice(2500).SaveX(ctx)
	f.service.Config.Currency.Unit = "USD"
	const api = "/v1/admin/payment"
	var callbacks, events []string
	const first = "https://first.example.test"
	const next = "https://next.example.test"
	const key = "fixture-merchant-account"
	backend := stripe.GetBackend(stripe.APIBackend)
	t.Cleanup(func() { stripe.SetBackend(stripe.APIBackend, backend) })
	transport := providerTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			return nil, fmt.Errorf("webhook management used the wrong merchant account")
		}
		var result string
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/webhook_endpoints":
			if err := r.ParseForm(); err != nil {
				return nil, err
			}
			callbacks = append(callbacks, r.Form.Get("url"))
			id := fmt.Sprintf("we_fixture_%d", len(callbacks))
			events = append(events, "create "+id)
			result = fmt.Sprintf(`{"id":%q,"secret":%q}`, id, "fixture-secret-"+id)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/webhook_endpoints/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/webhook_endpoints/")
			events = append(events, "delete "+id)
			result = fmt.Sprintf(`{"id":%q,"deleted":true}`, id)
		default:
			return nil, fmt.Errorf("unexpected webhook operation %s %s", r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(result))}, nil
	})
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{HTTPClient: &http.Client{Transport: transport}, MaxNetworkRetries: stripe.Int64(0)}))
	var created types.PaymentConfig
	body := fmt.Sprintf(`{"name":"Card","platform":"Stripe","enable":true,"domain":%q,"currency_unit":"usd","fee_mode":2,"fee_amount":25,"config":{"secret_key":%q,"payment":"card"}}`, first, key)
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, api+"/", operator, body, http.StatusOK), &created))
	method := f.client.Payment.GetX(ctx, created.Id)
	callback := "/v1/notify/Stripe/" + method.Token
	require.Equal(t, []string{first + callback}, callbacks)
	publicMethods := func() []types.PaymentMethod {
		t.Helper()
		var guest, member types.GetAvailablePaymentMethodsResponse
		for _, endpoint := range []struct {
			path, token string
			dest        *types.GetAvailablePaymentMethodsResponse
		}{
			{"/v1/public/portal/payment-method", "", &guest}, {"/v1/public/payment/methods", session, &member},
		} {
			data := f.request(http.MethodGet, endpoint.path, endpoint.token, "", http.StatusOK)
			require.NotContains(t, string(data), key)
			require.NotContains(t, string(data), "fixture-secret-")
			require.NoError(t, json.Unmarshal(data, endpoint.dest))
		}
		require.Equal(t, guest.List, member.List)
		return guest.List
	}
	quote := func(fee int64) {
		t.Helper()
		methods := publicMethods()
		require.Len(t, methods, 1)
		require.Equal(t, created.Id, methods[0].Id)
		require.Equal(t, fee, methods[0].FeeAmount)
		var preview types.PreOrderResponse
		body := fmt.Sprintf(`{"subscribe_id":"%d","quantity":1,"payment":"%d"}`, plan.ID, created.Id)
		require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/order/pre", session, body, http.StatusOK), &preview))
		require.EqualValues(t, fee, preview.FeeAmount)
		require.EqualValues(t, 2500+fee, preview.Amount)
	}
	update := func(domain string, enabled bool, fee int, code uint32) {
		t.Helper()
		payload := fmt.Sprintf(`{"id":"%d","name":"Card","platform":"Stripe","domain":%q,"enable":%t,"currency_unit":"usd","fee_mode":2,"fee_amount":%d,"config":{"payment":"card"}}`, created.Id, domain, enabled, fee)
		f.request(http.MethodPut, api+"/", operator, payload, code)
	}
	quote(25)
	update(first, true, 75, http.StatusOK)
	quote(75)
	require.Equal(t, []string{"create we_fixture_1"}, events)
	require.NoError(t, f.client.Driver().Exec(ctx, `CREATE TRIGGER reject_method_change BEFORE UPDATE ON payment BEGIN SELECT RAISE(ABORT, 'payment method unavailable'); END`, []any{}, nil))
	update(next, true, 90, xerr.DatabaseUpdateError)
	require.Equal(t, []string{"create we_fixture_1", "create we_fixture_2", "delete we_fixture_2"}, events)
	quote(75)
	require.NoError(t, f.client.Driver().Exec(ctx, `DROP TRIGGER reject_method_change`, []any{}, nil))
	update(next, true, 90, http.StatusOK)
	quote(90)
	require.Equal(t, []string{first + callback, next + callback, next + callback}, callbacks)
	require.Equal(t, []string{"create we_fixture_1", "create we_fixture_2", "delete we_fixture_2", "create we_fixture_3", "delete we_fixture_1"}, events)
	var listed types.GetPaymentMethodListResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, api+"/list?page=1&size=5&platform=Stripe", operator, "", http.StatusOK), &listed))
	require.Len(t, listed.List, 1)
	require.Equal(t, next+callback, listed.List[0].NotifyURL)
	update(next, false, 90, http.StatusOK)
	require.Empty(t, publicMethods())

	require.NoError(t, f.client.Driver().Exec(ctx, `CREATE TRIGGER reject_new_channel BEFORE INSERT ON payment BEGIN SELECT RAISE(ABORT, 'channel storage unavailable'); END`, []any{}, nil))
	priorEvents := len(events)
	f.request(http.MethodPost, api+"/", operator, body, xerr.DatabaseInsertError)
	require.Equal(t, []string{"create we_fixture_4", "delete we_fixture_4"}, events[priorEvents:])
	require.Equal(t, created.Id, f.client.Payment.Query().OnlyX(ctx).ID)
	require.Empty(t, publicMethods())
	require.NoError(t, f.client.Driver().Exec(ctx, `DROP TRIGGER reject_new_channel`, []any{}, nil))
	var replacement types.PaymentConfig
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, api+"/", operator, body, http.StatusOK), &replacement))
	published := publicMethods()
	require.Len(t, published, 1)
	require.Equal(t, replacement.Id, published[0].Id)
	require.NotEqual(t, created.Id, replacement.Id)
	var credentials struct {
		WebhookID string `json:"webhook_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(f.client.Payment.GetX(ctx, replacement.Id).Config), &credentials))
	require.Equal(t, "we_fixture_5", credentials.WebhookID)
}

func TestClosingRechargeRecoversStripeSessionBeforeCreditingWallet(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	inspector := f.startQueue()
	owner, session := f.account(false)
	wallet := f.client.UserWallet.Create().SetUserID(owner.ID).SetBalance(50).SaveX(ctx)
	method := f.client.Payment.Create().SetPlatform("Stripe").SetCurrencyUnit("USD").SetEnable(true).
		SetFeeMode(2).SetFeeAmount(30).SetConfig(`{"secret_key":"fixture-account","payment":"card"}`).SaveX(ctx)
	var recharge types.RechargeOrderResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/order/recharge", session, fmt.Sprintf(`{"amount":400,"payment":"%d"}`, method.ID), http.StatusOK), &recharge))
	invoice := f.client.Order.Query().OnlyX(ctx)
	require.Equal(t, recharge.OrderNo, invoice.OrderNo)
	require.EqualValues(t, 430, invoice.Amount)
	// The checkout process stopped after recording the amount but before saving the provider's reference.
	written, err := f.service.Store.Order().SetPaymentExpectation(ctx, invoice.OrderNo, invoice.Amount, "USD")
	require.NoError(t, err)
	require.True(t, written)
	const ref = "cs_lost_receipt"
	amount, status := 431, "unpaid"
	var searches int
	var lookup string
	transport := providerTransport(func(req *http.Request) (*http.Response, error) {
		sessionJSON := fmt.Sprintf(`{"id":%q,"client_reference_id":%q,"currency":"usd","amount_total":%d,"status":"complete","payment_status":%q,"payment_intent":{"status":"processing"}}`, ref, invoice.OrderNo, amount, status)
		switch req.Method + " " + req.URL.Path {
		case "GET /v1/checkout/sessions":
			searches++
			switch lookup {
			case "unavailable":
				return nil, fmt.Errorf("checkout list unavailable")
			case "not yet visible":
				sessionJSON = `{"object":"list","has_more":false,"data":[]}`
			default:
				sessionJSON = `{"object":"list","has_more":false,"data":[` + sessionJSON + `]}`
			}
		case "GET /v1/checkout/sessions/" + ref:
		default:
			return nil, fmt.Errorf("unexpected Stripe request %s %s", req.Method, req.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(sessionJSON))}, nil
	})
	original := stripe.GetBackend(stripe.APIBackend)
	t.Cleanup(func() { stripe.SetBackend(stripe.APIBackend, original) })
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{HTTPClient: &http.Client{Transport: transport}, MaxNetworkRetries: stripe.Int64(0)}))
	closeOrder := func(code uint32) {
		t.Helper()
		f.request(http.MethodPost, "/v1/public/order/close", session, fmt.Sprintf(`{"orderNo":%q}`, invoice.OrderNo), code)
	}
	for _, lookup = range []string{"unavailable", "not yet visible", "amount differs"} {
		closeOrder(xerr.ERROR)
		stored := f.client.Order.GetX(ctx, invoice.ID)
		require.Nil(t, stored.TradeNo)
		require.Equal(t, order.StatusPending, stored.Status)
		require.Equal(t, wallet.Balance, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
	}
	lookup = ""
	amount = 430
	closeOrder(xerr.ERROR)
	pending := f.client.Order.GetX(ctx, invoice.ID)
	require.NotNil(t, pending.TradeNo)
	require.Equal(t, ref, *pending.TradeNo)
	require.Equal(t, order.StatusPending, pending.Status)
	require.EqualValues(t, 50, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
	jobs, err := inspector.ListPendingTasks("default")
	require.NoError(t, err)
	require.Empty(t, jobs)
	queriesBeforeSettlement := searches
	status = "paid"
	closeOrder(http.StatusOK)
	closeOrder(http.StatusOK)
	require.Equal(t, queriesBeforeSettlement, searches, "a bound recharge should use its saved session")
	require.Equal(t, order.StatusPaid, f.client.Order.GetX(ctx, invoice.ID).Status)
	jobs, err = inspector.ListPendingTasks("default")
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.NoError(t, worker.NewActivateOrderLogic(f.service).ProcessTask(ctx, asynq.NewTask(jobs[0].Type, jobs[0].Payload)))
	closeOrder(http.StatusOK)
	require.Equal(t, order.StatusFinished, f.client.Order.GetX(ctx, invoice.ID).Status)
	require.EqualValues(t, 450, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
}

func TestClosingLegacyStripeRechargeReconcilesTheFinalIntent(t *testing.T) {
	for _, paidDuringCancel := range []bool{false, true} {
		name := "canceled"
		if paidDuringCancel {
			name = "paid during cancellation"
		}
		t.Run(name, func(t *testing.T) {
			f := newRouteFixture(t)
			ctx := t.Context()
			inspector := f.startQueue()
			owner, token := f.account(false)
			wallet := f.client.UserWallet.Create().SetUserID(owner.ID).SetBalance(35).SaveX(ctx)
			method := f.client.Payment.Create().SetPlatform("Stripe").SetCurrencyUnit("USD").SetEnable(true).SetConfig(`{"secret_key":"fixture-account"}`).SaveX(ctx)
			var invoice types.RechargeOrderResponse
			require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/order/recharge", token, fmt.Sprintf(`{"amount":400,"payment":"%d"}`, method.ID), http.StatusOK), &invoice))
			const intent = "pi_legacy_recharge"
			claimed, err := f.service.Store.Order().ClaimPaymentSession(ctx, invoice.OrderNo, intent, "")
			require.NoError(t, err)
			require.True(t, claimed)
			status, reference := "processing", invoice.OrderNo
			cancels := 0
			transport := providerTransport(func(r *http.Request) (*http.Response, error) {
				code := http.StatusOK
				switch r.Method + " " + r.URL.Path {
				case "GET /v1/payment_intents/" + intent:
				case "POST /v1/payment_intents/" + intent + "/cancel":
					cancels++
					status = "canceled"
					if paidDuringCancel {
						status, code = "succeeded", http.StatusBadRequest
					}
				default:
					return nil, fmt.Errorf("unexpected legacy payment request %s %s", r.Method, r.URL.Path)
				}
				body := fmt.Sprintf(`{"id":%q,"status":%q,"amount_received":400,"currency":"usd","metadata":{"order_no":%q}}`, intent, status, reference)
				if code != http.StatusOK {
					body = `{"error":{"message":"intent already settled"}}`
				}
				return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			saved := stripe.GetBackend(stripe.APIBackend)
			t.Cleanup(func() { stripe.SetBackend(stripe.APIBackend, saved) })
			stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{HTTPClient: &http.Client{Transport: transport}, MaxNetworkRetries: stripe.Int64(0)}))
			closeOrder := func(code uint32) {
				t.Helper()
				f.request(http.MethodPost, "/v1/public/order/close", token, fmt.Sprintf(`{"orderNo":%q}`, invoice.OrderNo), code)
			}
			reference = "different-invoice"
			closeOrder(xerr.ERROR)
			reference = invoice.OrderNo
			closeOrder(xerr.ERROR)
			require.Zero(t, cancels)
			require.Equal(t, order.StatusPending, f.client.Order.Query().OnlyX(ctx).Status)
			status = "requires_payment_method"
			closeOrder(http.StatusOK)
			closeOrder(http.StatusOK)
			require.Equal(t, 1, cancels)
			jobs, err := inspector.ListPendingTasks("default")
			require.NoError(t, err)
			if paidDuringCancel {
				require.Len(t, jobs, 1)
				require.Equal(t, queue.ForthwithActivateOrder, jobs[0].Type)
				require.NoError(t, worker.NewActivateOrderLogic(f.service).ProcessTask(ctx, asynq.NewTask(jobs[0].Type, jobs[0].Payload)))
				require.Equal(t, order.StatusFinished, f.client.Order.Query().OnlyX(ctx).Status)
				require.EqualValues(t, 435, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
			} else {
				require.Empty(t, jobs)
				require.Equal(t, order.StatusClosed, f.client.Order.Query().OnlyX(ctx).Status)
				require.Equal(t, wallet.Balance, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
			}
		})
	}
}

func TestDelayedStripeReceiptsCreditOnlyTheBoundRechargeOnce(t *testing.T) {
	for _, ref := range []string{"pi_legacy_wallet", "cs_current_wallet"} {
		t.Run(ref, func(t *testing.T) {
			f := newRouteFixture(t)
			ctx := t.Context()
			t.Setenv("GATEWAY_MODE", "false")
			RegisterNotifyHandlers(f.router, f.service)
			inspector := f.startQueue()
			owner, session := f.account(false)
			wallet := f.client.UserWallet.Create().SetUserID(owner.ID).SetBalance(25).SaveX(ctx)
			const signingKey = "fixture-webhook-key"
			method := f.client.Payment.Create().SetPlatform("Stripe").SetEnable(true).SetToken("wallet-test").
				SetConfig(`{"webhook_secret":"` + signingKey + `"}`).SetCurrencyUnit("USD").SetFeeMode(2).SetFeeAmount(25).SaveX(ctx)
			var recharge types.RechargeOrderResponse
			require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/order/recharge", session, fmt.Sprintf(`{"amount":475,"payment":"%d"}`, method.ID), http.StatusOK), &recharge))
			invoice := f.client.Order.Query().OnlyX(ctx)
			require.Equal(t, recharge.OrderNo, invoice.OrderNo)
			orders := f.service.Store.Order()
			ok, err := orders.SetPaymentExpectation(ctx, invoice.OrderNo, 500, "USD")
			require.NoError(t, err)
			require.True(t, ok)
			ok, err = orders.ClaimPaymentSession(ctx, invoice.OrderNo, ref, "")
			require.NoError(t, err)
			require.True(t, ok)

			send := func(event string, object map[string]any, want int) {
				t.Helper()
				payload, err := json.Marshal(map[string]any{"object": "event", "id": "evt_wallet", "type": event, "api_version": stripe.APIVersion, "data": map[string]any{"object": object}})
				require.NoError(t, err)
				now := time.Now()
				r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/notify/Stripe/wallet-test", strings.NewReader(string(payload)))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Stripe-Signature", fmt.Sprintf("t=%d,v1=%x", now.Unix(), webhook.ComputeSignature(now, payload, signingKey)))
				w := httptest.NewRecorder()
				f.router.ServeHTTP(w, r)
				require.Equal(t, want, w.Code, w.Body.String())
			}
			meta := map[string]string{"order_no": invoice.OrderNo, "user_id": fmt.Sprint(owner.ID)}
			intent := map[string]any{"id": "pi_legacy_wallet", "status": "succeeded", "amount_received": 500, "currency": "usd", "metadata": meta}
			event, receipt := "payment_intent.succeeded", intent
			var expectedJobs []string
			send("payment_intent.payment_failed", intent, http.StatusOK)
			if strings.HasPrefix(ref, "cs_") {
				send("payment_intent.succeeded", intent, http.StatusOK)
				receipt = map[string]any{"id": ref, "client_reference_id": invoice.OrderNo, "payment_status": "unpaid", "amount_total": 500, "currency": "usd", "metadata": meta, "payment_method_types": []string{"card"}}
				send("checkout.session.completed", receipt, http.StatusOK)
				for range 2 {
					send("checkout.session.async_payment_failed", receipt, http.StatusOK)
				}
				expectedJobs = append(expectedJobs, queue.DeferCloseOrder)
				event = "checkout.session.async_payment_succeeded"
				receipt["payment_status"] = "paid"
			}
			receipt["currency"] = "eur"
			send(event, receipt, http.StatusBadRequest)
			require.Equal(t, order.StatusPending, f.client.Order.GetX(ctx, invoice.ID).Status)
			require.Equal(t, wallet.Balance, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
			receipt["currency"] = "usd"
			send(event, receipt, http.StatusOK)
			send(event, receipt, http.StatusOK)
			require.Equal(t, order.StatusPaid, f.client.Order.GetX(ctx, invoice.ID).Status)
			jobs, err := inspector.ListPendingTasks("default")
			require.NoError(t, err)
			var kinds []string
			for _, job := range jobs {
				kinds = append(kinds, job.Type)
				if job.Type == queue.ForthwithActivateOrder {
					activation := asynq.NewTask(job.Type, job.Payload)
					for range 2 {
						require.NoError(t, worker.NewActivateOrderLogic(f.service).ProcessTask(ctx, activation))
					}
				}
			}
			require.ElementsMatch(t, append(expectedJobs, queue.ForthwithActivateOrder), kinds)
			send(event, receipt, http.StatusOK)
			require.Equal(t, order.StatusFinished, f.client.Order.GetX(ctx, invoice.ID).Status)
			require.EqualValues(t, 500, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
		})
	}
}

func TestReturningStripeCustomerCanReconcileAndFulfillRecharge(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	RegisterV2OrderHandlers(f.router.Hertz(), f.service)
	inspector := f.startQueue()
	owner, customer := f.account(false)
	f.service.Config.Currency.Unit = "USD"
	method := f.client.Payment.Create().SetPlatform("Stripe").SetEnable(true).SetCurrencyUnit("USD").
		SetConfig(`{"secret_key":"fixture-account","payment":"card"}`).SaveX(ctx)

	const reference = "cs_fixture_return"
	const cashier = "https://checkout.example.test/session"
	var receivedOrder string
	var creations, lookups int
	confirmed := false
	transport := providerTransport(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer fixture-account" {
			return nil, fmt.Errorf("checkout used unexpected merchant credentials")
		}
		var body string
		switch req.Method + " " + req.URL.Path {
		case "POST /v1/checkout/sessions":
			creations++
			if err := req.ParseForm(); err != nil {
				return nil, err
			}
			receivedOrder = req.Form.Get("client_reference_id")
			returnURL, err := url.Parse(req.Form.Get("success_url"))
			if err != nil {
				return nil, err
			}
			if receivedOrder == "" || returnURL.Host != "shop.example.test" || returnURL.Query().Get("order_no") != receivedOrder || req.Form.Get("line_items[0][price_data][unit_amount]") != "2350" || req.Form.Get("payment_intent_data[metadata][order_no]") != receivedOrder {
				return nil, fmt.Errorf("checkout omitted the order reference, return destination or charge amount")
			}
			body = fmt.Sprintf(`{"id":%q,"url":%q}`, reference, cashier)
		case "GET /v1/checkout/sessions/" + reference:
			lookups++
			status := "unpaid"
			if confirmed {
				status = "paid"
			}
			body = fmt.Sprintf(`{"id":%q,"client_reference_id":%q,"amount_total":2350,"currency":"usd","payment_status":%q}`, reference, receivedOrder, status)
		default:
			return nil, fmt.Errorf("unexpected Stripe operation %s %s", req.Method, req.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	original := stripe.GetBackend(stripe.APIBackend)
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{HTTPClient: &http.Client{Transport: transport}, MaxNetworkRetries: stripe.Int64(0)}))
	t.Cleanup(func() { stripe.SetBackend(stripe.APIBackend, original) })

	payload := fmt.Sprintf(`{"type":"recharge","amount":2350,"payment_id":%d,"return_url":"https://shop.example.test/wallet"}`, method.ID)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v2/public/orders", strings.NewReader(payload))
	req.Header.Set("Authorization", customer)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "recharge-stripe-fixture-1")
	var created types.V2OrderResponse
	require.NoError(t, json.Unmarshal(f.serve(req, http.StatusOK, http.StatusOK), &created))
	require.Equal(t, receivedOrder, created.Order.OrderNo)
	require.NotNil(t, created.Payment)
	require.Equal(t, cashier, created.Payment.CheckoutURL)
	path := "/v2/public/orders/" + receivedOrder
	var again types.V2OrderResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, path+"/checkout", customer, `{}`, http.StatusOK), &again))
	require.Equal(t, created.Payment, again.Payment)
	require.Equal(t, 1, creations)
	reconcile := func() types.V2OrderResponse {
		t.Helper()
		var got types.V2OrderResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodPost, path+"/reconcile", customer, `{}`, http.StatusOK), &got))
		return got
	}
	require.Equal(t, "pending_payment", reconcile().Order.Status)
	confirmed = true
	require.Equal(t, "pending_payment", reconcile().Order.Status)
	require.Equal(t, 1, lookups)
	// Expire the short return-lookup cooldown without sleeping between customer refreshes.
	require.NoError(t, f.service.Redis.Del(ctx, "order:stripe-return:"+receivedOrder).Err())
	require.Equal(t, "paid", reconcile().Order.PaymentStatus)
	require.Equal(t, 2, lookups)
	jobs, err := inspector.ListPendingTasks("default")
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.NoError(t, worker.NewActivateOrderLogic(f.service).ProcessTask(ctx, asynq.NewTask(jobs[0].Type, jobs[0].Payload)))
	require.Equal(t, "finished", reconcile().Order.FulfillmentStatus)
	f.request(http.MethodPost, path+"/checkout", customer, `{}`, xerr.OrderStatusError)
	wallet, err := f.service.Store.Wallet().FindOne(ctx, owner.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2350, wallet.Balance)
}
