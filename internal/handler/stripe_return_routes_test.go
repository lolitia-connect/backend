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

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	worker "github.com/perfect-panel/server/queue/logic/order"
	"github.com/stretchr/testify/require"
	stripe "github.com/stripe/stripe-go/v81"
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
	transport := providerTransport(func(req *http.Request) (*http.Response, error) {
		sessionJSON := fmt.Sprintf(`{"id":%q,"client_reference_id":%q,"currency":"usd","amount_total":%d,"status":"complete","payment_status":%q,"payment_intent":{"status":"processing"}}`, ref, invoice.OrderNo, amount, status)
		switch req.Method + " " + req.URL.Path {
		case "GET /v1/checkout/sessions":
			searches++
			sessionJSON = `{"object":"list","has_more":false,"data":[` + sessionJSON + `]}`
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
	closeOrder(xerr.ERROR)
	require.Nil(t, f.client.Order.GetX(ctx, invoice.ID).TradeNo)
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
	status = "paid"
	closeOrder(http.StatusOK)
	closeOrder(http.StatusOK)
	require.Equal(t, 2, searches)
	require.Equal(t, order.StatusPaid, f.client.Order.GetX(ctx, invoice.ID).Status)
	jobs, err = inspector.ListPendingTasks("default")
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.NoError(t, worker.NewActivateOrderLogic(f.service).ProcessTask(ctx, asynq.NewTask(jobs[0].Type, jobs[0].Payload)))
	closeOrder(http.StatusOK)
	require.Equal(t, order.StatusFinished, f.client.Order.GetX(ctx, invoice.ID).Status)
	require.EqualValues(t, 450, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
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
