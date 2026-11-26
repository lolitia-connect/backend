package handler

import (
	"crypto"
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
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/requestmeta"
	"github.com/perfect-panel/server/pkg/xerr"
	worker "github.com/perfect-panel/server/queue/logic/order"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/stretchr/testify/require"
)

func TestPublishedCallbackMatchesTheCashiersRechargeRequest(t *testing.T) {
	for _, channel := range []struct{ platform, config, domain, host string }{
		{"EPay", `{"pid":"1001","key":"merchant-only","url":"https://cashier.example.test","type":"alipay"}`, "", "store.example.test"},
		{"CryptoSaaS", `{"account_id":"1001","secret_key":"merchant-only","endpoint":"https://cashier.example.test","type":"alipay"}`, "https://billing.example.test/", "billing.example.test"},
	} {
		t.Run(channel.platform, func(t *testing.T) {
			f := newRouteFixture(t)
			f.startQueue()
			_, admin := f.account(true)
			_, member := f.account(false)
			f.service.Config.Host = "127.0.0.1"
			f.service.Config.Site.Host = "store.example.test\nbackup.example.test"
			f.service.Config.Site.SiteName = "Shop"
			f.service.Config.Currency.Unit = "CNY"
			t.Setenv("GATEWAY_PORT", "8181")
			const path = "/v1/admin/payment/"
			fields := fmt.Sprintf(`"name":"Cashier","platform":%q,"domain":%q,"config":%s,"currency_unit":"CNY","fee_mode":2,"fee_amount":25`, channel.platform, channel.domain, channel.config)
			var method types.PaymentConfig
			require.NoError(t, json.Unmarshal(f.request(http.MethodPost, path, admin, `{`+fields+`,"enable":false}`, http.StatusOK), &method))
			public := func() []types.PaymentMethod {
				t.Helper()
				var result types.GetAvailablePaymentMethodsResponse
				data := f.request(http.MethodGet, "/v1/public/payment/methods", member, "", http.StatusOK)
				require.NotContains(t, string(data), "merchant-only")
				require.NoError(t, json.Unmarshal(data, &result))
				return result.List
			}
			require.Empty(t, public())
			setEnabled := func(enabled bool) {
				t.Helper()
				f.request(http.MethodPut, path, admin, fmt.Sprintf(`{%s,"id":"%d","enable":%t}`, fields, method.Id, enabled), http.StatusOK)
			}
			setEnabled(true)
			available := public()
			require.Len(t, available, 1)
			require.Equal(t, method.Id, available[0].Id)
			require.EqualValues(t, 25, available[0].FeeAmount)
			stored := f.client.Payment.GetX(t.Context(), method.Id)
			for _, mode := range []struct{ gateway, prefix string }{{"false", ""}, {"true", "/api"}} {
				t.Setenv("GATEWAY_MODE", mode.gateway)
				var listing types.GetPaymentMethodListResponse
				require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path+"list?page=1&size=10", admin, "", http.StatusOK), &listing))
				require.Len(t, listing.List, 1)
				var recharge types.RechargeOrderResponse
				require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/order/recharge", member, fmt.Sprintf(`{"amount":500,"payment":"%d"}`, method.Id), http.StatusOK), &recharge))
				var checkout types.CheckoutOrderResponse
				require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/portal/order/checkout", member, fmt.Sprintf(`{"orderNo":%q,"returnUrl":"https://store.example.test/finish"}`, recharge.OrderNo), http.StatusOK), &checkout))
				u, err := url.Parse(checkout.CheckoutUrl)
				require.NoError(t, err)
				require.Equal(t, "cashier.example.test", u.Host)
				require.Equal(t, "5.25", u.Query().Get("money"))
				want := "https://" + channel.host + mode.prefix + "/v1/notify/" + channel.platform + "/" + stored.Token
				require.Equal(t, want, u.Query().Get("notify_url"))
				require.Equal(t, want, listing.List[0].NotifyURL)
			}
			setEnabled(false)
			require.Empty(t, public())
			if channel.domain == "" {
				f.service.Config.Site.Host = ""
				var incomplete types.GetPaymentMethodListResponse
				require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path+"list?page=1&size=10", admin, "", http.StatusOK), &incomplete))
				require.Len(t, incomplete.List, 1)
				require.Equal(t, method.Id, incomplete.List[0].Id)
				require.Empty(t, incomplete.List[0].NotifyURL)
				corrected := strings.Replace(fields, `"domain":""`, `"domain":"billing.example.test"`, 1)
				f.request(http.MethodPut, path, admin, fmt.Sprintf(`{%s,"id":"%d","enable":true}`, corrected, method.Id), http.StatusOK)
				var repaired types.GetPaymentMethodListResponse
				require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path+"list?page=1&size=10", admin, "", http.StatusOK), &repaired))
				require.Len(t, repaired.List, 1)
				require.Equal(t, "https://billing.example.test/api/v1/notify/EPay/"+stored.Token, repaired.List[0].NotifyURL)
				require.Len(t, public(), 1)
			}
		})
	}
}

func TestChannelEditPreservesAnExistingCashierQuote(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	f.startQueue()
	_, operator := f.account(true)
	_, member := f.account(false)
	f.service.Config.Currency.Unit = "USD"
	f.service.Config.Site.Host = "shop.example.test"
	f.service.Config.Site.SiteName = "Member wallet"
	t.Setenv("GATEWAY_MODE", "false")
	const channel = "/v1/admin/payment/"
	const settings = `"name":"Yuan cashier","platform":"EPay","enable":true,"currency_unit":"cny","fee_mode":2,"config":{"pid":"1001","key":"fixture","url":"https://cashier.example.test","type":"alipay"}`
	var method types.PaymentConfig
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, channel, operator, `{`+settings+`,"fee_amount":25,"exchange_rate":6}`, http.StatusOK), &method))
	invoice := func() string {
		t.Helper()
		var recharge types.RechargeOrderResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/order/recharge", member, fmt.Sprintf(`{"amount":1000,"payment":"%d"}`, method.Id), http.StatusOK), &recharge))
		return recharge.OrderNo
	}
	check := func(number, money string, fee, total, converted int64) {
		t.Helper()
		var checkout types.CheckoutOrderResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/portal/order/checkout", member, fmt.Sprintf(`{"orderNo":%q}`, number), http.StatusOK), &checkout))
		url, err := url.Parse(checkout.CheckoutUrl)
		require.NoError(t, err)
		require.Equal(t, "cashier.example.test", url.Host)
		require.Equal(t, number, url.Query().Get("out_trade_no"))
		require.Equal(t, money, url.Query().Get("money"))
		stored, err := f.service.Store.Order().FindOneByOrderNo(ctx, number)
		require.NoError(t, err)
		require.Equal(t, fee, stored.FeeAmount)
		require.Equal(t, total, stored.Amount)
		require.Equal(t, "CNY", stored.PaymentCurrency)
		require.Equal(t, converted, stored.PaymentAmount)
	}
	first := invoice()
	check(first, "61.50", 25, 1025, 6150)
	f.request(http.MethodPut, channel, operator, fmt.Sprintf(`{"id":"%d",%s,"fee_amount":50,"exchange_rate":7}`, method.Id, settings), http.StatusOK)
	var available types.GetAvailablePaymentMethodsResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/payment/methods", member, "", http.StatusOK), &available))
	require.Len(t, available.List, 1)
	require.Equal(t, method.Id, available.List[0].Id)
	require.Equal(t, 7.0, available.List[0].ExchangeRate)
	require.EqualValues(t, 50, available.List[0].FeeAmount)
	check(first, "61.50", 25, 1025, 6150)
	check(invoice(), "73.50", 50, 1050, 7350)

	f.service.Config.Currency.AccessKey = "rate-feed-fixture"
	feed := `{"success":false}`
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = providerTransport(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "api.apilayer.com", req.URL.Host)
		require.Equal(t, "/currency_data/convert", req.URL.Path)
		require.Equal(t, url.Values{"from": {"USD"}, "to": {"CNY"}, "amount": {"1"}}, req.URL.Query())
		require.Equal(t, "rate-feed-fixture", req.Header.Get("apikey"))
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(feed))}, nil
	})
	var automatic types.PaymentConfig
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, channel, operator, `{`+settings+`,"fee_amount":25}`, http.StatusOK), &automatic))
	require.Equal(t, 1.0, automatic.ExchangeRate)
	method = automatic
	feed = `{"success":true,"result":6.40}`
	quoted := invoice()
	check(quoted, "65.60", 25, 1025, 6560)
	feed = `{"success":true,"result":7.20}`
	var refreshed types.PaymentConfig
	require.NoError(t, json.Unmarshal(f.request(http.MethodPut, channel, operator, fmt.Sprintf(`{"id":"%d",%s,"fee_amount":25}`, method.Id, settings), http.StatusOK), &refreshed))
	require.Equal(t, 7.2, refreshed.ExchangeRate)
	feed = `{"success":false}`
	check(quoted, "65.60", 25, 1025, 6560)
	check(invoice(), "73.80", 25, 1025, 7380)
}

func TestEPayAPICheckoutReconcilesCancellationWithTheCashier(t *testing.T) {
	merchant, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	cashier, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	publicKey, err := x509.MarshalPKIXPublicKey(&cashier.PublicKey)
	require.NoError(t, err)
	config, err := json.Marshal(payment.EPayConfig{
		Pid: "1001", Url: "https://cashier.example.test", Type: "alipay", Version: "v2", CheckoutMode: "api",
		PrivateKey: base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(merchant)),
		PublicKey:  base64.StdEncoding.EncodeToString(publicKey),
	})
	require.NoError(t, err)
	for _, scenario := range []struct {
		name, payType, entry, checkoutType string
		paidDuringClose                    bool
	}{
		{"QR code", "qrcode", "https://cashier.example.test/code/475", "qr", false},
		{"browser checkout", "jump", "https://cashier.example.test/pay?receipt=475", "url", false},
		{"payment in the wallet during cancellation", "urlscheme", "alipays://platformapi/startapp?order=475", "url", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newRouteFixture(t)
			t.Setenv("GATEWAY_MODE", "false")
			f.service.Config.Site.Host = "store.example.test"
			f.service.Config.Site.SiteName = "Member wallet"
			f.service.Config.Currency.Unit = "CNY"
			ctx := t.Context()
			inspector := f.startQueue()
			owner, member := f.account(false)
			wallet := f.client.UserWallet.Create().SetUserID(owner.ID).SetBalance(15).SaveX(ctx)
			method := f.client.Payment.Create().SetPlatform("EPay").SetEnable(true).SetConfig(string(config)).SetToken("till").SetCurrencyUnit("CNY").SaveX(ctx)
			var recharge types.RechargeOrderResponse
			require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/order/recharge", member, fmt.Sprintf(`{"amount":615,"payment":"%d"}`, method.ID), http.StatusOK), &recharge))
			invoice := f.client.Order.Query().OnlyX(ctx)
			body := fmt.Sprintf(`{"orderNo":%q,"returnUrl":"https://store.example.test/receipt"}`, recharge.OrderNo)
			const trade = "receipt-475"
			money, status := "6.15", "0"
			var created, closed int
			previous := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previous })
			http.DefaultTransport = providerTransport(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, http.MethodPost, req.Method)
				require.Equal(t, "cashier.example.test", req.URL.Host)
				require.NoError(t, req.ParseForm())
				require.Equal(t, "1001", req.PostForm.Get("pid"))
				fields := map[string]any{"code": 0, "pid": "1001", "timestamp": strconv.FormatInt(time.Now().Unix(), 10)}
				switch req.URL.Path {
				case "/api/pay/create":
					created++
					require.Equal(t, recharge.OrderNo, req.PostForm.Get("out_trade_no"))
					require.Equal(t, "6.15", req.PostForm.Get("money"))
					require.Equal(t, "203.0.113.7", req.PostForm.Get("clientip"))
					fields["trade_no"], fields["pay_type"], fields["pay_info"] = trade, scenario.payType, scenario.entry
				case "/api/pay/query":
					require.Equal(t, trade, req.PostForm.Get("trade_no"))
					fields["trade_no"], fields["out_trade_no"], fields["type"], fields["money"], fields["status"] = trade, recharge.OrderNo, "alipay", money, status
				case "/api/pay/close":
					require.Equal(t, trade, req.PostForm.Get("trade_no"))
					closed++
					if scenario.paidDuringClose {
						status = "1"
						fields["code"] = -1
					}
				default:
					return nil, fmt.Errorf("unexpected cashier request %s", req.URL.Path)
				}
				var pairs []string
				for key, value := range fields {
					pairs = append(pairs, fmt.Sprintf("%s=%v", key, value))
				}
				sort.Strings(pairs)
				digest := sha256.Sum256([]byte(strings.Join(pairs, "&")))
				sig, err := rsa.SignPKCS1v15(rand.Reader, cashier, crypto.SHA256, digest[:])
				require.NoError(t, err)
				fields["sign"], fields["sign_type"] = base64.StdEncoding.EncodeToString(sig), "RSA"
				payload, err := json.Marshal(fields)
				require.NoError(t, err)
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(payload)))}, nil
			})
			for range 2 {
				req := httptest.NewRequestWithContext(requestmeta.With(ctx, requestmeta.Metadata{ClientIP: "203.0.113.7"}), http.MethodPost, "/v1/public/portal/order/checkout", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				var checkout types.CheckoutOrderResponse
				require.NoError(t, json.Unmarshal(f.serve(req, http.StatusOK, http.StatusOK), &checkout))
				require.Equal(t, types.CheckoutOrderResponse{Type: scenario.checkoutType, CheckoutUrl: scenario.entry}, checkout)
			}
			require.Equal(t, 1, created)
			const closePath = "/v1/public/order/close"
			money = "6.16"
			f.request(http.MethodPost, closePath, member, body, xerr.ERROR)
			require.Equal(t, order.StatusPending, f.client.Order.GetX(ctx, invoice.ID).Status)
			require.Zero(t, closed)
			money = "6.15"
			f.request(http.MethodPost, closePath, member, body, http.StatusOK)
			f.request(http.MethodPost, closePath, member, body, http.StatusOK)
			require.Equal(t, 1, closed)
			jobs, err := inspector.ListPendingTasks("default")
			require.NoError(t, err)
			if !scenario.paidDuringClose {
				require.Empty(t, jobs)
				require.Equal(t, order.StatusClosed, f.client.Order.GetX(ctx, invoice.ID).Status)
				require.Equal(t, wallet.Balance, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
				return
			}
			require.Equal(t, order.StatusPaid, f.client.Order.GetX(ctx, invoice.ID).Status)
			require.Len(t, jobs, 1)
			require.Equal(t, queue.ForthwithActivateOrder, jobs[0].Type)
			activate := worker.NewActivateOrderLogic(f.service)
			for range 2 {
				require.NoError(t, activate.ProcessTask(ctx, asynq.NewTask(jobs[0].Type, jobs[0].Payload)))
			}
			require.Equal(t, order.StatusFinished, f.client.Order.GetX(ctx, invoice.ID).Status)
			require.EqualValues(t, 630, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
		})
	}
}
