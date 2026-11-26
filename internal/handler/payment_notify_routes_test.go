package handler

import (
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alipay/global-open-sdk-go/com/alipay/api/tools"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	worker "github.com/perfect-panel/server/queue/logic/order"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/stretchr/testify/require"
)

func TestAlipayF2FRechargeSettlesAgainstTheCashierReceipt(t *testing.T) {
	merchant, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	cashier, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	publicDER, err := x509.MarshalPKIXPublicKey(&cashier.PublicKey)
	require.NoError(t, err)
	config, err := json.Marshal(map[string]string{
		"app_id": "fixture-shop", "invoice_name": "Wallet credit", "sandbox": "true",
		"private_key": base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(merchant)),
		"public_key":  base64.StdEncoding.EncodeToString(publicDER),
	})
	require.NoError(t, err)
	for _, scenario := range []string{"notification", "cancel unpaid QR", "payment during cancellation", "delayed cashier receipt"} {
		t.Run(scenario, func(t *testing.T) {
			f := newRouteFixture(t)
			ctx := t.Context()
			t.Setenv("GATEWAY_MODE", "false")
			RegisterNotifyHandlers(f.router, f.service)
			inspector := f.startQueue()
			member, session := f.account(false)
			wallet := f.client.UserWallet.Create().SetUserID(member.ID).SetBalance(120).SaveX(ctx)
			f.service.Config.Site.Host = "shop.example.test"
			f.service.Config.Currency.Unit = "CNY"
			method := f.client.Payment.Create().SetPlatform("AlipayF2F").SetEnable(true).SetToken("counter").SetConfig(string(config)).
				SetCurrencyUnit("CNY").SetFeeMode(2).SetFeeAmount(15).SaveX(ctx)
			var created types.RechargeOrderResponse
			require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/order/recharge", session, fmt.Sprintf(`{"amount":650,"payment":"%d"}`, method.ID), http.StatusOK), &created))
			pay := fmt.Sprintf(`{"orderNo":%q}`, created.OrderNo)
			const callback = "/v1/notify/AlipayF2F/counter"
			const qr = "https://qr.example.test/counter"
			sign := func(data string) string {
				t.Helper()
				digest := sha256.Sum256([]byte(data))
				signature, err := rsa.SignPKCS1v15(rand.Reader, cashier, crypto.SHA256, digest[:])
				require.NoError(t, err)
				return base64.StdEncoding.EncodeToString(signature)
			}
			state := "WAIT_BUYER_PAY"
			var operations []string
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			http.DefaultTransport = providerTransport(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, "openapi-sandbox.dl.alipaydev.com", r.URL.Host)
				require.Equal(t, http.MethodPost, r.Method)
				require.NoError(t, r.ParseForm())
				require.Equal(t, "fixture-shop", r.Form.Get("app_id"))
				var trade map[string]any
				require.NoError(t, json.Unmarshal([]byte(r.Form.Get("biz_content")), &trade))
				require.Equal(t, created.OrderNo, trade["out_trade_no"])
				operation := r.Form.Get("method")
				operations = append(operations, operation)
				response := map[string]string{"code": "10000", "msg": "Success", "out_trade_no": created.OrderNo}
				switch operation {
				case "alipay.trade.precreate":
					require.Equal(t, "Wallet credit", trade["subject"])
					require.Equal(t, "6.65", trade["total_amount"])
					require.Equal(t, "https://shop.example.test"+callback, r.Form.Get("notify_url"))
					response["qr_code"] = qr
				case "alipay.trade.query":
					if scenario == "delayed cashier receipt" && state == "WAIT_BUYER_PAY" {
						response["code"], response["sub_code"] = "40004", "ACQ.SYSTEM_ERROR"
					} else {
						response["trade_no"], response["total_amount"], response["trade_status"] = "cashier-57", "6.65", state
					}
				case "alipay.trade.close":
					if scenario == "payment during cancellation" {
						state = "TRADE_SUCCESS"
						response["code"], response["sub_code"] = "40004", "ACQ.TRADE_STATUS_ERROR"
					}
				default:
					return nil, fmt.Errorf("unexpected cashier operation %q", operation)
				}
				raw, err := json.Marshal(response)
				require.NoError(t, err)
				body := fmt.Sprintf(`{%q:%s,"sign":%q}`, strings.ReplaceAll(operation, ".", "_")+"_response", raw, sign(string(raw)))
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			var checkout types.CheckoutOrderResponse
			require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/portal/order/checkout", "", pay, http.StatusOK), &checkout))
			require.Equal(t, types.CheckoutOrderResponse{Type: "qr", CheckoutUrl: qr}, checkout)
			invoice := f.client.Order.Query().OnlyX(ctx)
			require.Equal(t, "CNY", invoice.PaymentCurrency)
			require.EqualValues(t, 665, invoice.PaymentAmount)
			notify := func(status string) string {
				t.Helper()
				form := url.Values{"out_trade_no": {created.OrderNo}, "trade_no": {"cashier-57"}, "trade_status": {status}, "total_amount": {"6.65"}}
				form.Set("sign", sign(form.Encode()))
				form.Set("sign_type", "RSA2")
				req := httptest.NewRequestWithContext(ctx, http.MethodPost, callback, strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				rec := httptest.NewRecorder()
				f.router.ServeHTTP(rec, req)
				require.Equal(t, http.StatusOK, rec.Code)
				return rec.Body.String()
			}
			wantOps := []string{"alipay.trade.precreate"}
			if scenario == "notification" {
				require.Equal(t, "success", notify("WAIT_BUYER_PAY"))
				require.Equal(t, order.StatusPending, f.client.Order.GetX(ctx, invoice.ID).Status)
				require.NoError(t, f.service.Queue.Close())
				var failure struct {
					Code uint32 `json:"code"`
				}
				require.NoError(t, json.Unmarshal([]byte(notify("TRADE_SUCCESS")), &failure))
				require.EqualValues(t, xerr.ERROR, failure.Code)
				require.Equal(t, order.StatusPaid, f.client.Order.GetX(ctx, invoice.ID).Status)
				require.EqualValues(t, 120, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
				pending, err := inspector.ListPendingTasks("default")
				require.NoError(t, err)
				require.Empty(t, pending)
				f.startQueue()
				require.Equal(t, "success", notify("TRADE_SUCCESS"))
			} else if scenario == "delayed cashier receipt" {
				const closePath = "/v1/public/order/close"
				f.request(http.MethodPost, closePath, session, pay, xerr.ERROR)
				require.Equal(t, order.StatusPending, f.client.Order.GetX(ctx, invoice.ID).Status)
				require.Equal(t, wallet.Balance, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
				pending, err := inspector.ListPendingTasks("default")
				require.NoError(t, err)
				require.Empty(t, pending)
				state = "TRADE_SUCCESS"
				f.request(http.MethodPost, closePath, session, pay, http.StatusOK)
				wantOps = append(wantOps, "alipay.trade.query", "alipay.trade.query")
			} else {
				f.request(http.MethodPost, "/v1/public/order/close", session, pay, http.StatusOK)
				wantOps = append(wantOps, "alipay.trade.query", "alipay.trade.close")
				if scenario == "payment during cancellation" {
					wantOps = append(wantOps, "alipay.trade.query")
				}
			}
			require.Equal(t, wantOps, operations)
			jobs, err := inspector.ListPendingTasks("default")
			require.NoError(t, err)
			if scenario == "cancel unpaid QR" {
				require.Empty(t, jobs)
				require.Equal(t, order.StatusClosed, f.client.Order.GetX(ctx, invoice.ID).Status)
				require.EqualValues(t, 120, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
				return
			}
			require.Equal(t, order.StatusPaid, f.client.Order.GetX(ctx, invoice.ID).Status)
			require.Len(t, jobs, 1)
			require.Equal(t, queue.ForthwithActivateOrder, jobs[0].Type)
			require.NoError(t, worker.NewActivateOrderLogic(f.service).ProcessTask(ctx, asynq.NewTask(jobs[0].Type, jobs[0].Payload)))
			require.Equal(t, "success", notify("TRADE_SUCCESS"))
			require.Equal(t, order.StatusFinished, f.client.Order.GetX(ctx, invoice.ID).Status)
			require.EqualValues(t, 770, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
		})
	}
}

func TestAlipayPlusRechargeCreditsTheWalletOnceOnFinalNotification(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	RegisterNotifyHandlers(f.router, f.service)
	inspector := f.startQueue()
	owner, customer := f.account(false)
	wallet := f.client.UserWallet.Create().SetUserID(owner.ID).SetBalance(75).SaveX(ctx)
	providerKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	privateDER, err := x509.MarshalPKCS8PrivateKey(providerKey)
	require.NoError(t, err)
	publicDER, err := x509.MarshalPKIXPublicKey(&providerKey.PublicKey)
	require.NoError(t, err)
	const clientID = "fixture-cashier"
	const hosted = "https://cashier.example.test/checkout/april"
	const callback = "/v1/notify/AlipayPlus/cashier-notify"
	t.Setenv("GATEWAY_MODE", "false")
	f.service.Config.Site.Host = "shop.example.test"
	f.service.Config.Currency.Unit = "USD"
	var recharge types.RechargeOrderResponse
	requests := make(chan string, 3)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PaymentRequestID string                           `json:"paymentRequestId"`
			PaymentAmount    struct{ Currency, Value string } `json:"paymentAmount"`
			Notify           string                           `json:"paymentNotifyUrl"`
			Redirect         string                           `json:"paymentRedirectUrl"`
		}
		if r.Method != http.MethodPost || r.URL.Path != "/ams/api/v1/payments/pay" || json.NewDecoder(r.Body).Decode(&req) != nil ||
			req.PaymentAmount.Currency != "USD" || req.PaymentAmount.Value != "1225" || req.Notify != "https://shop.example.test"+callback || req.Redirect != "https://shop.example.test/receipt" {
			http.Error(w, "incorrect cashier request", http.StatusBadRequest)
			return
		}
		var result string
		if len(requests) == 0 {
			result = `{"result":{"resultCode":"MERCHANT_NOT_READY","resultStatus":"F","resultMessage":"Try again"}}`
		} else {
			result = fmt.Sprintf(`{"result":{"resultCode":"PAYMENT_IN_PROCESS","resultStatus":"U"},"paymentRequestId":%q,"normalUrl":%q}`, req.PaymentRequestID, hosted)
		}
		requests <- req.PaymentRequestID
		stamp := fmt.Sprint(time.Now().UnixMilli())
		signature, err := tools.GenSign(r.Method, r.URL.Path, clientID, stamp, result, base64.StdEncoding.EncodeToString(privateDER))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Client-Id", clientID)
		w.Header().Set("Response-Time", stamp)
		w.Header().Set("Signature", "algorithm=RSA256,keyVersion=1,signature="+signature)
		_, _ = io.WriteString(w, result)
	}))
	t.Cleanup(gateway.Close)
	cfg, err := json.Marshal(map[string]string{"client_id": clientID, "gateway_url": gateway.URL, "payment_method": "ALIPAY_HK", "private_key": base64.StdEncoding.EncodeToString(privateDER), "alipay_public_key": base64.StdEncoding.EncodeToString(publicDER)})
	require.NoError(t, err)
	method := f.client.Payment.Create().SetPlatform("AlipayPlus").SetEnable(true).SetToken("cashier-notify").SetCurrencyUnit("USD").
		SetFeeMode(2).SetFeeAmount(25).SetConfig(string(cfg)).SaveX(ctx)
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/order/recharge", customer, fmt.Sprintf(`{"amount":1200,"payment":"%d"}`, method.ID), http.StatusOK), &recharge))
	invoice := f.client.Order.Query().OnlyX(ctx)
	require.Equal(t, recharge.OrderNo, invoice.OrderNo)
	require.EqualValues(t, 1225, invoice.Amount)
	checkout := fmt.Sprintf(`{"orderNo":%q,"returnUrl":"https://shop.example.test/receipt"}`, recharge.OrderNo)
	const pay = "/v1/public/portal/order/checkout"
	f.request(http.MethodPost, pay, customer, checkout, xerr.ERROR)
	unpaid := f.client.Order.GetX(ctx, invoice.ID)
	require.Equal(t, order.StatusPending, unpaid.Status)
	require.Nil(t, unpaid.PaymentSession)
	require.EqualValues(t, 75, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
	for range 2 {
		var session types.CheckoutOrderResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodPost, pay, customer, checkout, http.StatusOK), &session))
		require.Equal(t, types.CheckoutOrderResponse{Type: "url", CheckoutUrl: hosted}, session)
	}
	require.Len(t, requests, 2)
	require.Equal(t, recharge.OrderNo, <-requests)
	require.Equal(t, recharge.OrderNo, <-requests)

	notify := func(kind string) {
		t.Helper()
		const path = "/v1/notify/AlipayPlus/cashier-notify"
		body := fmt.Sprintf(`{"notifyType":%q,"result":{"resultCode":"SUCCESS","resultStatus":"S","resultMessage":"Success"},"paymentRequestId":%q,"paymentAmount":{"value":"12.25","currency":"USD"}}`, kind, invoice.OrderNo)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, path, strings.NewReader(body))
		stamp := time.Now().UTC().Format(time.RFC3339)
		signature, err := tools.GenSign(http.MethodPost, path, clientID, stamp, body, base64.StdEncoding.EncodeToString(privateDER))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Client-Id", clientID)
		req.Header.Set("Request-Time", stamp)
		req.Header.Set("Signature", "algorithm=RSA256,keyVersion=1,signature="+signature)
		response := httptest.NewRecorder()
		f.router.ServeHTTP(response, req)
		require.Equal(t, http.StatusOK, response.Code)
		require.Equal(t, clientID, response.Header().Get("Client-Id"))
		require.JSONEq(t, `{"result":{"resultCode":"SUCCESS","resultStatus":"S","resultMessage":"Success"}}`, response.Body.String())
	}
	notify("PAYMENT_PENDING")
	require.Equal(t, order.StatusPending, f.client.Order.GetX(ctx, invoice.ID).Status)
	pending, err := inspector.ListPendingTasks("default")
	require.NoError(t, err)
	require.Empty(t, pending)
	notify("PAYMENT_RESULT")
	notify("PAYMENT_RESULT")
	require.Equal(t, order.StatusPaid, f.client.Order.GetX(ctx, invoice.ID).Status)
	pending, err = inspector.ListPendingTasks("default")
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, queue.ForthwithActivateOrder, pending[0].Type)
	require.NoError(t, worker.NewActivateOrderLogic(f.service).ProcessTask(ctx, asynq.NewTask(pending[0].Type, pending[0].Payload)))
	notify("PAYMENT_RESULT")
	require.Equal(t, order.StatusFinished, f.client.Order.GetX(ctx, invoice.ID).Status)
	require.EqualValues(t, 1275, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
}

func TestCryptomusRechargesOnlyAfterProviderConfirmsPayment(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	t.Setenv("GATEWAY_MODE", "false")
	RegisterNotifyHandlers(f.router, f.service)
	inspector := f.startQueue()
	owner, customer := f.account(false)
	_, admin := f.account(true)
	wallet := f.client.UserWallet.Create().SetUserID(owner.ID).SetBalance(120).SaveX(ctx)
	f.service.Config.Site.Host = "store.example.test"
	f.service.Config.Currency.Unit = "USD"
	method := f.client.Payment.Create().SetPlatform("Cryptomus").SetEnable(true).SetToken("local-callback").SetCurrencyUnit("USD").
		SetConfig(`{"merchant_id":"fixture-shop","api_key":"fixture-key"}`).SetFeeMode(2).SetFeeAmount(20).SaveX(ctx)
	var recharge types.RechargeOrderResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/order/recharge", customer, fmt.Sprintf(`{"amount":1234,"payment":"%d"}`, method.ID), http.StatusOK), &recharge))
	invoice := f.client.Order.Query().OnlyX(ctx)
	require.Equal(t, invoice.OrderNo, recharge.OrderNo)
	const callback = "/v1/notify/Cryptomus/local-callback"
	const cashier = "https://cashier.example.test/pay/invoice-1"
	const receipt = "invoice-1"
	// The provider signs the original JSON bytes, including their field order.
	sign := func(body string) string {
		return fmt.Sprintf("%x", md5.Sum([]byte(base64.StdEncoding.EncodeToString([]byte(body))+"fixture-key")))
	}
	providerStatus := "process"
	var created, lookups int
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = providerTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Host != "api.cryptomus.com" {
			return nil, fmt.Errorf("unexpected provider endpoint %s %s", r.Method, r.URL.Host)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		if r.Header.Get("merchant") != "fixture-shop" || r.Header.Get("sign") != sign(string(raw)) {
			return nil, fmt.Errorf("unsigned merchant request")
		}
		var fields map[string]string
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
		switch r.URL.Path {
		case "/v1/payment":
			created++
			if fields["amount"] != "12.54" || fields["currency"] != "USD" || fields["order_id"] != invoice.OrderNo || fields["url_callback"] != "https://store.example.test"+callback {
				return nil, fmt.Errorf("incorrect invoice request: %v", fields)
			}
		case "/v1/payment/info":
			lookups++
			if fields["uuid"] != receipt {
				return nil, fmt.Errorf("wrong invoice reference: %v", fields)
			}
		default:
			return nil, fmt.Errorf("unknown provider operation %s", r.URL.Path)
		}
		body := fmt.Sprintf(`{"state":0,"result":{"uuid":%q,"order_id":%q,"amount":"12.540000","currency":"USD","url":%q,"payment_status":%q,"is_final":%t}}`, receipt, invoice.OrderNo, cashier, providerStatus, providerStatus != "process")
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	for range 2 {
		var checkout types.CheckoutOrderResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/portal/order/checkout", "", fmt.Sprintf(`{"orderNo":%q}`, invoice.OrderNo), http.StatusOK), &checkout))
		require.Equal(t, cashier, checkout.CheckoutUrl)
	}
	require.Equal(t, 1, created)
	recorded := f.client.Order.GetX(ctx, invoice.ID)
	require.EqualValues(t, 1254, recorded.PaymentAmount)
	require.Equal(t, "USD", recorded.PaymentCurrency)
	require.NotNil(t, recorded.TradeNo)
	require.Equal(t, receipt, *recorded.TradeNo)
	notify := func(status string, final bool) {
		t.Helper()
		unsigned := fmt.Sprintf(`{"type":"payment","uuid":%q,"order_id":%q,"status":%q,"is_final":%t}`, receipt, invoice.OrderNo, status, final)
		body := strings.TrimSuffix(unsigned, "}") + `,"sign":"` + sign(unsigned) + `"}`
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, callback, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		f.router.ServeHTTP(recorder, req)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Equal(t, "OK", recorder.Body.String())
	}
	before := lookups
	notify("process", false)
	require.Equal(t, before, lookups)
	providerStatus = "wrong_amount"
	notify(providerStatus, true)
	require.EqualValues(t, order.StatusPending, f.client.Order.GetX(ctx, invoice.ID).Status)
	require.EqualValues(t, 120, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
	var review types.FilterUnmatchedPaymentLogResponse
	path := fmt.Sprintf("/v1/admin/log/unmatched-payment/list?page=1&size=10&user_id=%d", owner.ID)
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path, admin, "", http.StatusOK), &review))
	require.Len(t, review.List, 1)
	require.Equal(t, invoice.OrderNo, review.List[0].OrderNo)
	require.Equal(t, receipt, review.List[0].TradeNo)
	require.Contains(t, review.List[0].Reason, "wrong_amount")
	providerStatus = "paid"
	notify(providerStatus, true)
	notify(providerStatus, true)
	jobs, err := inspector.ListPendingTasks("default")
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, queue.ForthwithActivateOrder, jobs[0].Type)
	require.NoError(t, worker.NewActivateOrderLogic(f.service).ProcessTask(ctx, asynq.NewTask(jobs[0].Type, jobs[0].Payload)))
	notify(providerStatus, true)
	require.EqualValues(t, order.StatusFinished, f.client.Order.GetX(ctx, invoice.ID).Status)
	require.EqualValues(t, 1354, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
}

func TestCryptomusCheckoutRecoversAnUnacknowledgedInvoice(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	t.Setenv("GATEWAY_MODE", "false")
	f.service.Config.Site.Host = "store.example.test"
	f.service.Config.Currency.Unit = "USD"
	method := f.client.Payment.Create().SetPlatform("Cryptomus").SetEnable(true).SetToken("recovery").
		SetConfig(`{"merchant_id":"fixture-shop","api_key":"fixture-key"}`).SaveX(ctx)
	invoice := f.client.Order.Create().SetOrderNo("lost-provider-reply").SetPaymentID(method.ID).
		SetMethod("Cryptomus").SetAmount(435).SetStatus(order.StatusPending).SaveX(ctx)
	const receipt = "recovered-invoice"
	const cashier = "https://cashier.example.test/pay/recovered"
	orderNo := "different-order"
	var refs []string
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = providerTransport(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "api.cryptomus.com", r.URL.Host)
		var fields map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&fields))
		switch r.URL.Path {
		case "/v1/payment":
			require.Equal(t, invoice.OrderNo, fields["order_id"])
			require.Equal(t, "4.35", fields["amount"])
			refs = append(refs, "create")
			return nil, io.ErrUnexpectedEOF
		case "/v1/payment/info":
			require.Equal(t, map[string]string{"order_id": invoice.OrderNo}, fields)
			refs = append(refs, invoice.OrderNo)
		default:
			return nil, fmt.Errorf("unexpected provider path %s", r.URL.Path)
		}
		body := fmt.Sprintf(`{"state":0,"result":{"uuid":%q,"order_id":%q,"amount":"4.35","currency":"USD","url":%q}}`, receipt, orderNo, cashier)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	const path = "/v1/public/portal/order/checkout"
	body := fmt.Sprintf(`{"orderNo":%q}`, invoice.OrderNo)
	f.request(http.MethodPost, path, "", body, xerr.ERROR)
	current := f.client.Order.GetX(ctx, invoice.ID)
	require.Nil(t, current.TradeNo)
	require.EqualValues(t, order.StatusPending, current.Status)
	orderNo = invoice.OrderNo
	for range 2 {
		var checkout types.CheckoutOrderResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodPost, path, "", body, http.StatusOK), &checkout))
		require.Equal(t, cashier, checkout.CheckoutUrl)
	}
	current = f.client.Order.GetX(ctx, invoice.ID)
	require.NotNil(t, current.TradeNo)
	require.Equal(t, receipt, *current.TradeNo)
	require.EqualValues(t, 435, current.PaymentAmount)
	require.Equal(t, "USD", current.PaymentCurrency)
	require.Equal(t, []string{"create", invoice.OrderNo, "create", invoice.OrderNo}, refs)
}

func TestClosingCryptomusPurchaseFollowsConfirmedInvoice(t *testing.T) {
	for _, final := range []string{"cancel", "paid"} {
		t.Run(final, func(t *testing.T) {
			f := newRouteFixture(t)
			ctx := t.Context()
			t.Setenv("GATEWAY_MODE", "false")
			inspector := f.startQueue()
			f.service.Config.Currency.Unit = "USD"
			f.service.Config.Site.Host = "shop.example.test"
			buyer, session := f.account(false)
			wallet := f.client.UserWallet.Create().SetUserID(buyer.ID).SetBalance(70).SetGiftAmount(250).SaveX(ctx)
			plan := f.client.Subscribe.Create().SetName("Regional").SetSell(true).SetInventory(1).SetUnitPrice(1000).SetUnitTime("Month").SaveX(ctx)
			coupon := f.client.Coupon.Create().SetCode("WELCOME").SetType(2).SetDiscount(100).SetCount(1).SetExpireTime(time.Now().Add(time.Hour).UnixMilli()).SaveX(ctx)
			method := f.client.Payment.Create().SetPlatform("Cryptomus").SetEnable(true).SetCurrencyUnit("USD").SetExchangeRate(1).SetToken("fixture-store").SetConfig(`{"merchant_id":"store","api_key":"fixture-only"}`).SaveX(ctx)
			var purchased types.PurchaseOrderResponse
			body := fmt.Sprintf(`{"subscribe_id":"%d","quantity":1,"payment":"%d","coupon":"WELCOME"}`, plan.ID, method.ID)
			require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/order/purchase", session, body, http.StatusOK), &purchased))
			invoice := f.client.Order.Query().OnlyX(ctx)
			require.Equal(t, purchased.OrderNo, invoice.OrderNo)
			require.EqualValues(t, 650, invoice.Amount)
			status, received, isFinal := "process", "0.00", false
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			const cashier = "https://cashier.example.test/regional"
			http.DefaultTransport = providerTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodPost || r.URL.Host != "api.cryptomus.com" {
					return nil, fmt.Errorf("unexpected provider request: %s %s", r.Method, r.URL.Host)
				}
				var fields map[string]string
				if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
					return nil, err
				}
				switch r.URL.Path {
				case "/v1/payment":
					if fields["order_id"] != invoice.OrderNo || fields["amount"] != "6.50" || fields["currency"] != "USD" {
						return nil, fmt.Errorf("invoice does not match the reserved purchase")
					}
				case "/v1/payment/info":
					if fields["uuid"] != "regional-invoice" {
						return nil, fmt.Errorf("lookup used a different invoice")
					}
				default:
					return nil, fmt.Errorf("unexpected operation %s", r.URL.Path)
				}
				response := fmt.Sprintf(`{"state":0,"result":{"uuid":"regional-invoice","order_id":%q,"amount":"6.50","currency":"USD","url":%q,"status":%q,"payment_amount":%q,"is_final":%t}}`, invoice.OrderNo, cashier, status, received, isFinal)
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
			})
			orderRef := fmt.Sprintf(`{"orderNo":%q}`, invoice.OrderNo)
			var checkout types.CheckoutOrderResponse
			require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/portal/order/checkout", "", orderRef, http.StatusOK), &checkout))
			require.Equal(t, cashier, checkout.CheckoutUrl)
			closeOrder := func(code uint32) {
				t.Helper()
				f.request(http.MethodPost, "/v1/public/order/close", session, orderRef, code)
			}
			assertReservations := func(gift, stock, uses int64) {
				t.Helper()
				funds := f.client.UserWallet.GetX(ctx, wallet.ID)
				require.EqualValues(t, 70, funds.Balance)
				require.EqualValues(t, gift, funds.GiftAmount)
				require.EqualValues(t, stock, f.client.Subscribe.GetX(ctx, plan.ID).Inventory)
				require.EqualValues(t, uses, f.client.Coupon.GetX(ctx, coupon.ID).UsedCount)
			}
			closeOrder(xerr.ERROR)
			status, received, isFinal = "cancel", "0.50", true
			closeOrder(xerr.ERROR)
			assertReservations(0, 0, 1)
			require.EqualValues(t, order.StatusPending, f.client.Order.GetX(ctx, invoice.ID).Status)
			status, received = final, "0.00"
			if final == "paid" {
				received = "6.50"
			}
			closeOrder(http.StatusOK)
			closeOrder(http.StatusOK)
			jobs, err := inspector.ListPendingTasks("default")
			require.NoError(t, err)
			if final == "cancel" {
				require.Empty(t, jobs)
				assertReservations(250, 1, 0)
				require.Zero(t, f.client.UserSubscribe.Query().CountX(ctx))
				require.EqualValues(t, order.StatusClosed, f.client.Order.GetX(ctx, invoice.ID).Status)
				return
			}
			require.Len(t, jobs, 1)
			require.Equal(t, queue.ForthwithActivateOrder, jobs[0].Type)
			require.NoError(t, worker.NewActivateOrderLogic(f.service).ProcessTask(ctx, asynq.NewTask(jobs[0].Type, jobs[0].Payload)))
			assertReservations(0, 0, 1)
			sub := f.client.UserSubscribe.Query().OnlyX(ctx)
			require.Equal(t, buyer.ID, sub.UserID)
			require.Equal(t, plan.ID, sub.SubscribeID)
			require.EqualValues(t, order.StatusFinished, f.client.Order.GetX(ctx, invoice.ID).Status)
		})
	}
}

func TestEPayCompatibleGatewaysConfirmReceiptsBeforeSettlement(t *testing.T) {
	for _, tc := range []struct{ platform, config string }{
		{"EPay", `{"pid":"1001","key":"local-key","url":"https://cashier.example.test","type":"alipay"}`},
		{"CryptoSaaS", `{"account_id":"1001","secret_key":"local-key","endpoint":"https://cashier.example.test","type":"alipay"}`},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			f := newRouteFixture(t)
			ctx := t.Context()
			t.Setenv("GATEWAY_MODE", "false")
			RegisterNotifyHandlers(f.router, f.service)
			inspector := f.startQueue()
			f.service.Config.Currency.Unit = "CNY"
			f.service.Config.Site.Host = "store.example.test"
			f.service.Config.Site.SiteName = "Gift credit"
			owner, customer := f.account(false)
			method := f.client.Payment.Create().SetPlatform(tc.platform).SetEnable(true).SetConfig(tc.config).SetCurrencyUnit("CNY").SetToken("fixture-shop").SaveX(ctx)
			body := fmt.Sprintf(`{"amount":799,"payment":"%d"}`, method.ID)
			var recharge types.RechargeOrderResponse
			require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/order/recharge", customer, body, http.StatusOK), &recharge))
			var checkout types.CheckoutOrderResponse
			require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/portal/order/checkout", "", fmt.Sprintf(`{"orderNo":%q,"returnUrl":"https://store.example.test/finish"}`, recharge.OrderNo), http.StatusOK), &checkout))
			redirect, err := url.Parse(checkout.CheckoutUrl)
			require.NoError(t, err)
			require.Equal(t, "cashier.example.test", redirect.Host)
			require.Equal(t, "7.99", redirect.Query().Get("money"))
			require.Equal(t, recharge.OrderNo, redirect.Query().Get("out_trade_no"))
			callback, err := url.Parse(redirect.Query().Get("notify_url"))
			require.NoError(t, err)
			require.Equal(t, "store.example.test", callback.Host)
			const trade = "gateway-trade-407"
			params := url.Values{"pid": {"1001"}, "type": {"alipay"}, "trade_no": {trade}, "out_trade_no": {recharge.OrderNo}, "money": {"7.99"}, "trade_status": {"TRADE_SUCCESS"}}
			signature := md5.Sum([]byte(params.Encode() + "local-key"))
			params.Set("sign_type", "MD5")
			params.Set("sign", fmt.Sprintf("%x", signature))
			status, checks := 0, 0
			old := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = old })
			http.DefaultTransport = providerTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Host != "cashier.example.test" || r.URL.Path != "/api.php" || r.URL.Query().Get("out_trade_no") != recharge.OrderNo {
					return nil, fmt.Errorf("unexpected gateway lookup")
				}
				checks++
				body := fmt.Sprintf(`{"code":1,"pid":"1001","status":%d,"trade_no":%q,"out_trade_no":%q,"money":"7.99","type":"alipay"}`, status, trade, recharge.OrderNo)
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			notify := func(want int) {
				t.Helper()
				recorder := httptest.NewRecorder()
				f.router.ServeHTTP(recorder, httptest.NewRequestWithContext(ctx, http.MethodGet, callback.Path+"?"+params.Encode(), nil))
				require.Equal(t, want, recorder.Code, recorder.Body.String())
				if want == http.StatusOK {
					require.Equal(t, "success", recorder.Body.String())
				}
			}
			notify(http.StatusBadRequest)
			require.EqualValues(t, order.StatusPending, f.client.Order.Query().OnlyX(ctx).Status)
			status = 1
			notify(http.StatusOK)
			notify(http.StatusOK)
			require.Equal(t, 3, checks)
			jobs, err := inspector.ListPendingTasks("default")
			require.NoError(t, err)
			require.Len(t, jobs, 1)
			require.Equal(t, queue.ForthwithActivateOrder, jobs[0].Type)
			require.NoError(t, worker.NewActivateOrderLogic(f.service).ProcessTask(ctx, asynq.NewTask(jobs[0].Type, jobs[0].Payload)))
			funds, err := f.service.Store.Wallet().FindOne(ctx, owner.ID)
			require.NoError(t, err)
			require.EqualValues(t, 799, funds.Balance)
		})
	}
}
