package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/cloudwego/hertz/pkg/protocol/sse"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/logic/notify"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/orderstream"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	worker "github.com/perfect-panel/server/queue/logic/order"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/stretchr/testify/require"
)

func TestRejectedBillingEditsPreserveTheMembersQuote(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	owner, session := f.account(false)
	wallet := f.client.UserWallet.Create().SetUserID(owner.ID).SetGiftAmount(75).SetBalance(850).SaveX(ctx)
	plan := f.client.Subscribe.Create().SetName("Monthly").SetUnitPrice(500).SetSell(true).SetInventory(4).SaveX(ctx)
	method := f.client.Payment.Create().SetName("Card").SetPlatform("Stripe").SetConfig(`{}`).SetEnable(true).SetFeeMode(2).SetFeeAmount(25).SaveX(ctx)
	voucher := f.client.Coupon.Create().SetName("Member discount").SetCode("MEMBER100").SetType(2).SetDiscount(100).SetCount(2).SetExpireTime(time.Now().Add(time.Hour).UnixMilli()).SaveX(ctx)
	body := fmt.Sprintf(`{"subscribe_id":"%d","quantity":2,"payment":"%d","coupon":%q}`, plan.ID, method.ID, voucher.Code)
	checkQuote := func() {
		t.Helper()
		var quote types.PreOrderResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/order/pre", session, body, http.StatusOK), &quote))
		require.EqualValues(t, 1000, quote.Price)
		require.EqualValues(t, 100, quote.CouponDiscount)
		require.EqualValues(t, 25, quote.FeeAmount)
		require.EqualValues(t, 75, quote.GiftAmount)
		require.EqualValues(t, 850, quote.Amount)
	}
	checkQuote()
	for _, edit := range []struct{ path, form string }{
		{"subscribe/", fmt.Sprintf(`{"id":"%d","name":"New price","unit_price":999,"sell":"yes"}`, plan.ID)},
		{"coupon/", fmt.Sprintf(`{"id":"%d","name":"Revised offer","discount":200,"type":256}`, voucher.ID)},
		{"payment/", fmt.Sprintf(`{"id":"%d","name":"New fee","platform":"Stripe","config":{},"fee_amount":50,"enable":1}`, method.ID)},
		{"user/basic", fmt.Sprintf(`{"user_id":"%d","gift_amount":0,"balance":2000,"referral_percentage":300}`, owner.ID)},
	} {
		f.request(http.MethodPut, "/v1/admin/"+edit.path, operator, edit.form, xerr.InvalidParams)
		checkQuote()
	}
	require.Equal(t, "Monthly", f.client.Subscribe.GetX(ctx, plan.ID).Name)
	require.Equal(t, "Card", f.client.Payment.GetX(ctx, method.ID).Name)
	require.Equal(t, "Member discount", f.client.Coupon.GetX(ctx, voucher.ID).Name)
	funds := f.client.UserWallet.GetX(ctx, wallet.ID)
	require.Equal(t, wallet.Balance, funds.Balance)
	require.Equal(t, wallet.GiftAmount, funds.GiftAmount)
	require.Zero(t, f.client.Order.Query().CountX(ctx))
	require.Zero(t, f.client.SystemLog.Query().CountX(ctx))
}

func TestPublishedCouponCanBeQuotedAndReservedWithinItsWindow(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	f.startQueue()
	_, operator := f.account(true)
	owner, session := f.account(false)
	wallet := f.client.UserWallet.Create().SetUserID(owner.ID).SetBalance(900).SetGiftAmount(75).SaveX(ctx)
	plan := f.client.Subscribe.Create().SetName("Monthly").SetUnitPrice(500).SetInventory(4).SetSell(true).SaveX(ctx)
	other := f.client.Subscribe.Create().SetName("Annual").SetUnitPrice(4000).SetInventory(1).SetSell(true).SaveX(ctx)
	method := f.client.Payment.Create().SetPlatform("Stripe").SetConfig(`{}`).SetEnable(true).SaveX(ctx)
	const code = "SEASONAL"
	now := time.Now()
	coupon := fmt.Sprintf(`{"name":"Seasonal offer","code":%q,"type":2,"discount":100,"start_time":%d,"expire_time":%d,"count":2,"user_limit":1,"subscribe":["%d"]}`, code, now.Add(-time.Hour).UnixMilli(), now.Add(time.Hour).UnixMilli(), plan.ID)
	f.request(http.MethodPost, "/v1/admin/coupon/", operator, coupon, http.StatusOK)
	const member = "/v1/public/order"
	const guest = "/v1/public/portal"
	post := func(path, token string, planID int64, want uint32) json.RawMessage {
		t.Helper()
		body := fmt.Sprintf(`{"subscribe_id":"%d","payment":"%d","quantity":2,"coupon":%q`, planID, method.ID, code)
		if path == guest+"/purchase" {
			body += `,"auth_type":"email","identifier":"seasonal@example.test","password":"coupon-buyer-fixture"`
		}
		return f.request(http.MethodPost, path, token, body+"}", want)
	}
	routes := []struct{ path, token string }{{guest + "/pre", ""}, {guest + "/purchase", ""}, {member + "/pre", session}, {member + "/purchase", session}}
	voucher := f.client.Coupon.Query().OnlyX(ctx)
	publish := func(start, end time.Time, enabled bool) {
		t.Helper()
		body, err := json.Marshal(types.UpdateCouponRequest{Id: voucher.ID, Name: voucher.Name, Code: code, Count: 2, Type: 2, Discount: 100,
			StartTime: start.UnixMilli(), ExpireTime: end.UnixMilli(), UserLimit: 1, Subscribe: []string{fmt.Sprint(plan.ID)}, Enable: &enabled})
		require.NoError(t, err)
		f.request(http.MethodPut, "/v1/admin/coupon/", operator, string(body), http.StatusOK)
	}
	for _, offer := range []struct {
		start, end time.Duration
		enabled    bool
		want       uint32
	}{{time.Hour, 2 * time.Hour, true, xerr.CouponNotApplicable}, {-2 * time.Hour, -time.Hour, true, xerr.CouponExpired}, {-time.Hour, time.Hour, false, xerr.CouponDisabled}} {
		publish(now.Add(offer.start), now.Add(offer.end), offer.enabled)
		for _, route := range routes {
			post(route.path, route.token, plan.ID, offer.want)
		}
	}
	require.EqualValues(t, 4, f.client.Subscribe.GetX(ctx, plan.ID).Inventory)
	require.EqualValues(t, 75, f.client.UserWallet.GetX(ctx, wallet.ID).GiftAmount)
	publish(now.Add(-time.Hour), now.Add(time.Hour), true)
	for _, route := range routes {
		post(route.path, route.token, other.ID, xerr.CouponNotApplicable)
	}
	require.Zero(t, f.client.Order.Query().CountX(ctx))
	require.Zero(t, f.client.Coupon.Query().OnlyX(ctx).UsedCount)
	require.EqualValues(t, 1, f.client.Subscribe.GetX(ctx, other.ID).Inventory)
	for _, buyer := range []struct {
		base, session string
		payable       int64
	}{{member, session, 825}, {guest, "", 900}} {
		var quote types.PreOrderResponse
		require.NoError(t, json.Unmarshal(post(buyer.base+"/pre", buyer.session, plan.ID, http.StatusOK), &quote))
		require.EqualValues(t, 1000, quote.Price)
		require.EqualValues(t, 100, quote.CouponDiscount)
		require.Equal(t, buyer.payable, quote.Amount)
		var created types.PurchaseOrderResponse
		require.NoError(t, json.Unmarshal(post(buyer.base+"/purchase", buyer.session, plan.ID, http.StatusOK), &created))
		invoice, err := f.service.Store.Order().FindOneByOrderNo(ctx, created.OrderNo)
		require.NoError(t, err)
		require.True(t, invoice.CouponReserved)
		require.Equal(t, code, invoice.Coupon)
		require.Equal(t, quote.Amount, invoice.Amount)
		if buyer.session != "" {
			post(member+"/purchase", session, plan.ID, xerr.CouponInsufficientUsage)
			require.EqualValues(t, 1, f.client.Coupon.Query().OnlyX(ctx).UsedCount)
		}
	}
	post(guest+"/purchase", "", plan.ID, xerr.CouponInsufficientUsage)
	require.Equal(t, 2, f.client.Order.Query().CountX(ctx))
	require.EqualValues(t, 2, f.client.Subscribe.GetX(ctx, plan.ID).Inventory)
	require.EqualValues(t, 2, f.client.Coupon.Query().OnlyX(ctx).UsedCount)
	funds := f.client.UserWallet.GetX(ctx, wallet.ID)
	require.EqualValues(t, 900, funds.Balance)
	require.Zero(t, funds.GiftAmount)
}

func TestGuestCheckoutDeliversSubscriptionAndAccountSession(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	RegisterV2OrderHandlers(f.router.Hertz(), f.service)
	f.service.Config.Site.SiteName = "Checkout fixture"
	f.service.Config.Site.Host = "store.example.test"
	f.service.Config.Currency.Unit = "CNY"
	f.service.Config.JwtAuth.AccessExpire = 300
	inspector := f.startQueue()
	plan := f.client.Subscribe.Create().SetName("One day").SetSell(true).SetInventory(2).
		SetUnitPrice(250).SetUnitTime("Hour").SetTraffic(1024).SaveX(ctx)
	method := f.client.Payment.Create().SetPlatform("EPay").SetEnable(true).SetCurrencyUnit("CNY").SetExchangeRate(1).
		SetFeeMode(3).SetFeePercent(3).SetFeeAmount(30).
		SetConfig(`{"pid":"1001","key":"fixture-key","url":"https://cashier.example.test","type":"alipay"}`).SaveX(ctx)
	checkout := types.V2CreateOrderRequest{Type: "purchase", PaymentID: method.ID, SubscribeID: plan.ID, Quantity: 24,
		ReturnURL: "https://store.example.test/return", Guest: &types.V2GuestOrderRequest{AuthType: "email", Identifier: "buyer@example.test", Password: "guest-purchase-fixture"}}
	const orders = "/v2/public/orders"
	create := func(payload types.V2CreateOrderRequest, httpStatus int, code uint32) types.V2OrderResponse {
		t.Helper()
		body, err := json.Marshal(payload)
		require.NoError(t, err)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, orders, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "fixture-guest-purchase-001")
		data := f.serve(req, httpStatus, code)
		var response types.V2OrderResponse
		if code == http.StatusOK {
			require.NoError(t, json.Unmarshal(data, &response))
		} else {
			require.Empty(t, data)
		}
		return response
	}
	// The order commits, but its first checkout response cannot load the invoice.
	loseFirstRead := true
	f.client.Order.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
			result, err := next.Query(ctx, q)
			if invoices, ok := result.([]*ent.Order); loseFirstRead && ok && len(invoices) > 0 && err == nil {
				loseFirstRead = false
				return nil, fmt.Errorf("invoice read interrupted")
			}
			return result, err
		})
	}))
	create(checkout, http.StatusOK, xerr.DatabaseQueryError)
	committed := f.client.Order.Query().OnlyX(ctx)
	require.EqualValues(t, 1, f.client.Subscribe.GetX(ctx, plan.ID).Inventory)
	created := create(checkout, http.StatusOK, http.StatusOK)
	require.NotEmpty(t, created.CheckoutToken)
	require.Equal(t, committed.OrderNo, created.Order.OrderNo)
	require.Equal(t, "pending_payment", created.Order.Status)
	require.Equal(t, int64(6210), created.Order.Amount)
	require.NotNil(t, created.Payment)
	redirect, err := url.Parse(created.Payment.CheckoutURL)
	require.NoError(t, err)
	require.Equal(t, "cashier.example.test", redirect.Host)
	require.Equal(t, "62.10", redirect.Query().Get("money"))
	require.Equal(t, created.Order.OrderNo, redirect.Query().Get("out_trade_no"))
	retry := create(checkout, http.StatusOK, http.StatusOK)
	require.Equal(t, created.Order.OrderNo, retry.Order.OrderNo)
	require.Equal(t, created.CheckoutToken, retry.CheckoutToken)
	checkout.Quantity++
	create(checkout, http.StatusConflict, xerr.InvalidParams)
	invoice := f.client.Order.Query().OnlyX(ctx)
	require.EqualValues(t, 1, f.client.Subscribe.GetX(ctx, plan.ID).Inventory)
	require.Zero(t, f.client.User.Query().CountX(ctx))

	path := orders + "/" + created.Order.OrderNo
	legacyReceipt := func(status uint8) types.QueryPurchaseOrderResponse {
		t.Helper()
		query := url.Values{"order_no": {created.Order.OrderNo}, "checkout_token": {created.CheckoutToken}}
		var receipt types.QueryPurchaseOrderResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/portal/order/status?"+query.Encode(), "", "", http.StatusOK), &receipt))
		require.Equal(t, created.Order.OrderNo, receipt.OrderNo)
		require.Equal(t, created.Order.Amount, receipt.Amount)
		require.Equal(t, plan.ID, receipt.Subscribe.Id)
		require.Equal(t, method.ID, receipt.Payment.Id)
		require.Equal(t, status, receipt.Status)
		return receipt
	}
	require.Empty(t, legacyReceipt(order.StatusPending).Token)
	capability := fmt.Sprintf(`{"checkout_token":%q}`, created.CheckoutToken)
	f.request(http.MethodPost, path+"/session", "", capability, xerr.OrderStatusError)
	_, admin := f.account(true)
	f.request(http.MethodPut, "/v1/admin/order/status", admin, fmt.Sprintf(`{"id":"%d","status":2,"trade_no":"fixture-receipt"}`, invoice.ID), http.StatusOK)
	var paid types.V2OrderResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, path+"/reconcile", "", capability, http.StatusOK), &paid))
	require.Equal(t, "paid", paid.Order.PaymentStatus)
	require.Equal(t, "pending", paid.Order.FulfillmentStatus)
	require.Empty(t, legacyReceipt(order.StatusPaid).Token)
	f.request(http.MethodPost, path+"/session", "", capability, xerr.OrderStatusError)
	tasks, err := inspector.ListPendingTasks("default")
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, queue.ForthwithActivateOrder, tasks[0].Type)
	require.NoError(t, worker.NewActivateOrderLogic(f.service).ProcessTask(ctx, asynq.NewTask(tasks[0].Type, tasks[0].Payload)))

	var finished types.V2OrderResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path+"?checkout_token="+url.QueryEscape(created.CheckoutToken), "", "", http.StatusOK), &finished))
	require.Equal(t, "finished", finished.Order.FulfillmentStatus)
	require.Greater(t, finished.Order.StateVersion, paid.Order.StateVersion)
	identity := f.client.UserAuthMethod.Query().OnlyX(ctx)
	require.Equal(t, checkout.Guest.Identifier, identity.AuthIdentifier)
	sub := f.client.UserSubscribe.Query().OnlyX(ctx)
	require.Equal(t, identity.UserID, sub.UserID)
	require.Equal(t, invoice.ID, sub.OrderID)
	require.Equal(t, plan.ID, sub.SubscribeID)
	require.Equal(t, 24*time.Hour, sub.ExpireTime.Sub(sub.StartTime))
	var session types.V2OrderSessionResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, path+"/session", "", capability, http.StatusOK), &session))
	require.NotEmpty(t, session.AccessToken)
	var methods types.GetOAuthMethodsResponse
	for _, token := range []string{session.AccessToken, legacyReceipt(order.StatusFinished).Token} {
		require.NotEmpty(t, token)
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/user/oauth_methods", token, "", http.StatusOK), &methods))
		require.Len(t, methods.Methods, 1)
		require.Equal(t, identity.AuthIdentifier, methods.Methods[0].AuthIdentifier)
	}
}

type orderStreamClient struct {
	bytes.Buffer
	onFlush func([]byte)
}

func (s *orderStreamClient) Flush() error {
	s.onFlush(s.Bytes())
	return nil
}

func (*orderStreamClient) Finalize() error { return nil }

func readOrderStream(t *testing.T, f *routeFixture, url, lastID, through string, onFlush func([]byte)) *sse.Reader {
	t.Helper()
	ctx, disconnect := context.WithTimeout(t.Context(), 5*time.Second)
	defer disconnect()
	client := &orderStreamClient{onFlush: func(data []byte) {
		if onFlush != nil {
			onFlush(data)
		}
		if bytes.Contains(data, []byte(through)) {
			disconnect()
		}
	}}
	raw := app.NewContext(0)
	raw.Request.Header.SetMethod(http.MethodGet)
	raw.Request.SetRequestURI(url)
	raw.Request.Header.SetHost("panel.example.test")
	raw.Request.Header.Set("Last-Event-ID", lastID)
	raw.Response.HijackWriter(client)
	f.router.Hertz().ServeHTTP(ctx, raw)
	require.Equal(t, http.StatusOK, raw.Response.StatusCode())
	require.Equal(t, "no", string(raw.Response.Header.Peek("X-Accel-Buffering")))
	var response protocol.Response
	response.Header.SetContentTypeBytes(raw.Response.Header.ContentType())
	response.SetBody(client.Bytes())
	reader, err := sse.NewReader(&response)
	require.NoError(t, err)
	return reader
}

func TestOrderStreamResumesAfterPaymentWithTheCommittedWalletCredit(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	RegisterV2OrderHandlers(f.router.Hertz(), f.service)
	inspector := f.startQueue()
	owner, session := f.account(false)
	wallet := f.client.UserWallet.Create().SetUserID(owner.ID).SetBalance(50).SaveX(ctx)
	row := f.client.Payment.Create().SetPlatform("EPay").SetCurrencyUnit("CNY").SetEnable(true).SetConfig(`{}`).SaveX(ctx)
	method, err := f.service.Store.Payment().FindOne(ctx, row.ID)
	require.NoError(t, err)
	var recharge types.RechargeOrderResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/order/recharge", session, fmt.Sprintf(`{"amount":435,"payment":"%d"}`, row.ID), http.StatusOK), &recharge))
	var ticket types.V2EventTicketResponse
	path := "/v2/public/orders/" + recharge.OrderNo + "/event-ticket"
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, path, session, `{}`, http.StatusOK), &ticket))
	created := f.client.OrderEvent.Query().OnlyX(ctx)
	charged := false
	first := readOrderStream(t, f, ticket.URL, "", order.EventOrderPaymentPaid, func(data []byte) {
		if !charged && bytes.Contains(data, []byte(order.EventOrderCreated)) {
			charged = true
			require.NoError(t, notify.SettleConfirmedPayment(ctx, f.service, method, recharge.OrderNo, "receipt-435", 435, "CNY"))
		}
	})
	readEvent := func(reader *sse.Reader, kind string, version int64, paymentStatus, fulfillment string) string {
		t.Helper()
		var event sse.Event
		require.NoError(t, reader.Read(&event))
		require.Equal(t, kind, event.Type)
		var state order.EventPayload
		require.NoError(t, json.Unmarshal(event.Data, &state))
		require.Equal(t, recharge.OrderNo, state.OrderNo)
		require.Equal(t, version, state.StateVersion)
		require.Equal(t, paymentStatus, state.PaymentStatus)
		require.Equal(t, fulfillment, state.FulfillmentStatus)
		return event.ID
	}
	readEvent(first, "order.snapshot", 1, "pending", "not_started")
	require.Equal(t, fmt.Sprint(created.ID), readEvent(first, order.EventOrderCreated, 1, "pending", "not_started"))
	cursor := readEvent(first, order.EventOrderPaymentPaid, 2, "paid", "pending")
	journal, err := f.service.Store.OrderEvent().ListAfter(ctx, recharge.OrderNo, created.ID, 10)
	require.NoError(t, err)
	require.Len(t, journal, 1)
	require.Equal(t, fmt.Sprint(journal[0].Id), cursor)
	var end sse.Event
	require.ErrorIs(t, first.Read(&end), io.EOF)
	require.Equal(t, wallet.Balance, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
	jobs, err := inspector.ListPendingTasks("default")
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, queue.ForthwithActivateOrder, jobs[0].Type)
	activation := asynq.NewTask(jobs[0].Type, jobs[0].Payload)
	for range 2 {
		require.NoError(t, worker.NewActivateOrderLogic(f.service).ProcessTask(ctx, activation))
	}
	// The notification is only a wake-up. The reconnecting client must recover the
	// durable fulfillment even when its first journal reads are interrupted.
	var interrupted int
	f.client.OrderEvent.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(queryCtx context.Context, q ent.Query) (ent.Value, error) {
			if interrupted < 2 {
				interrupted++
				if interrupted == 2 {
					orderstream.Wake(ctx, f.service.Redis, recharge.OrderNo)
				}
				return nil, fmt.Errorf("order journal temporarily unavailable")
			}
			return next.Query(queryCtx, q)
		})
	}))
	second := readOrderStream(t, f, ticket.URL, cursor, order.EventOrderFulfilled, nil)
	require.Equal(t, 2, interrupted)
	readEvent(second, "order.snapshot", 3, "paid", "finished")
	remaining, err := f.service.Store.OrderEvent().ListAfter(ctx, recharge.OrderNo, journal[0].Id, 10)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Equal(t, fmt.Sprint(remaining[0].Id), readEvent(second, order.EventOrderFulfilled, 3, "paid", "finished"))
	require.ErrorIs(t, second.Read(&end), io.EOF)
	require.EqualValues(t, 485, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
}

func TestOrderStreamResetsAStaleCursorAndReplaysOnlyTheOwnersReceipt(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	RegisterV2OrderHandlers(f.router.Hertz(), f.service)
	owner, session := f.account(false)
	_, other := f.account(false)
	invoice := f.client.Order.Create().SetOrderNo("return-43").SetUserID(owner.ID).SetAmount(435).
		SetStatus(order.StatusPaid).SetStateVersion(2).SaveX(ctx)
	const receipt = `{"receipt":"cleared-43"}`
	f.client.OrderEvent.Create().SetID(10).SetOrderNo("another-order").SetEventType("order.closed").SetPayload(`{"unrelated":true}`).SaveX(ctx)
	paid := f.client.OrderEvent.Create().SetID(11).SetOrderNo(invoice.OrderNo).SetEventType("order.payment_paid").SetPayload(receipt).SaveX(ctx)
	path := "/v2/public/orders/" + invoice.OrderNo + "/event-ticket"
	f.request(http.MethodPost, path, other, `{}`, xerr.InvalidAccess)
	var ticket types.V2EventTicketResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, path, session, `{}`, http.StatusOK), &ticket))
	require.Greater(t, ticket.TicketExpiresAt, time.Now().Unix())

	reader := readOrderStream(t, f, ticket.URL, "7", receipt, nil)
	for _, kind := range []string{"order.snapshot", "order.reset"} {
		var event sse.Event
		require.NoError(t, reader.Read(&event))
		require.Equal(t, kind, event.Type)
		var snapshot types.V2OrderSnapshot
		require.NoError(t, json.Unmarshal(event.Data, &snapshot))
		require.Equal(t, invoice.OrderNo, snapshot.OrderNo)
		require.Equal(t, "paid", snapshot.PaymentStatus)
		require.Equal(t, "pending", snapshot.FulfillmentStatus)
		require.EqualValues(t, 2, snapshot.StateVersion)
	}
	var event sse.Event
	require.NoError(t, reader.Read(&event))
	require.Equal(t, "order.payment_paid", event.Type)
	require.Equal(t, fmt.Sprint(paid.ID), event.ID)
	require.JSONEq(t, receipt, string(event.Data))
	require.ErrorIs(t, reader.Read(&event), io.EOF)
}
