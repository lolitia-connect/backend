package epay

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testV2Gateway(t *testing.T) (*Gateway, *rsa.PrivateKey) {
	t.Helper()
	merchant, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	platform, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey(&platform.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := Open(GatewayConfig{Version: "v2", Endpoint: "https://pay.example/shop", MerchantID: "1001", PrivateKey: base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(merchant)), PublicKey: base64.StdEncoding.EncodeToString(public)})
	if err != nil {
		t.Fatal(err)
	}
	return gateway, platform
}

func TestV2SignedFieldsAndTimestampWindow(t *testing.T) {
	gateway, platform := testV2Gateway(t)
	now := time.Unix(1800000000, 0)
	gateway.now = func() time.Time { return now }
	params := map[string]string{"pid": "1001", "timestamp": strconv.FormatInt(now.Unix(), 10), "sign_type": "RSA", "money": "10.29", "zero": "0"}
	params["sign"], _ = signV2(platform, params)
	if err := gateway.verifyV2(params); err != nil {
		t.Fatal(err)
	}
	params["money"] = "10.28"
	if err := gateway.verifyV2(params); err == nil {
		t.Fatal("tampered amount accepted")
	}
	params["sign"], _ = signV2(platform, params)
	gateway.now = func() time.Time { return now.Add(301 * time.Second) }
	if err := gateway.verifyV2(params); err == nil {
		t.Fatal("stale signature accepted")
	}
	form, err := gateway.signed(map[string]string{"money": "0.29"})
	if err != nil || form.Get("pid") != "1001" || form.Get("sign_type") != "RSA" || form.Get("sign") == "" {
		t.Fatalf("signed form = %v, %v", form, err)
	}
	if _, err := gateway.signed(map[string]string{"pid": "other"}); err == nil {
		t.Fatal("caller overrode merchant ID")
	}
}

func TestV2CanonicalScalarsAndAmbiguousResponses(t *testing.T) {
	canonical, err := canonicalV2(map[string]string{"zero": "0", "space": " \t", "value": " a & b ", "sign": "omit"})
	if err != nil || canonical != "value= a & b &zero=0" {
		t.Fatalf("canonical = %q, %v", canonical, err)
	}
	for _, body := range []string{`{"code":0,"code":1}`, `{"code":0} {}`, `[]`} {
		if _, err := responseFields([]byte(body)); err == nil {
			t.Fatalf("ambiguous JSON accepted: %s", body)
		}
	}
	fields, err := responseFields([]byte(`{"code":0,"trade_no":"9223372036854775808","data":{"unsigned":true},"money":"0.29"}`))
	if err != nil {
		t.Fatal(err)
	}
	params, err := responseScalars(fields)
	if err != nil || params["trade_no"] != "9223372036854775808" || params["money"] != "0.29" {
		t.Fatalf("scalar values = %v, %v", params, err)
	}
	if _, exists := params["data"]; exists {
		t.Fatal("unsigned nested data exposed as signed scalar")
	}
	if _, err := canonicalV2(map[string]string{"array[0]": "value"}); err == nil {
		t.Fatal("array key accepted as scalar")
	}
	if _, err := Open(GatewayConfig{Version: "v2", Endpoint: "http://pay.example", MerchantID: "1001"}); err == nil || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal("insecure V2 endpoint accepted or secret disclosed")
	}
}

func TestV1APICheckoutAndReceipt(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if err := request.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		client := NewClient("1001", "", "shared-key", "alipay")
		switch request.URL.Path {
		case "/shop/mapi.php":
			params := make(map[string]string)
			for name := range request.PostForm {
				params[name] = request.PostForm.Get(name)
			}
			if request.Method != http.MethodPost || !client.VerifySign(params) || params["money"] != "10.29" || params["clientip"] != "203.0.113.9" || params["device"] != "jump" {
				t.Errorf("unexpected V1 create: %v", params)
			}
			writer.Write([]byte(`{"code":1,"trade_no":"trade","qrcode":"https://cashier.example/qr"}`))
		case "/shop/api.php":
			if request.Form.Get("act") != "order" || request.Form.Get("out_trade_no") != "order" || request.Form.Get("key") != "shared-key" {
				t.Errorf("unexpected V1 query: %v", request.Form)
			}
			writer.Write([]byte(`{"code":1,"pid":1001,"trade_no":"trade","out_trade_no":"order","type":"alipay","money":"10.29","status":0}`))
		default:
			t.Errorf("unexpected path %s", request.URL.Path)
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	gateway, err := Open(GatewayConfig{Endpoint: server.URL + "/shop", CheckoutMode: "api", MerchantID: "1001", Key: "shared-key", PaymentType: "alipay"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := gateway.CreatePayment(context.Background(), "Plan", "order", "10.29", "https://panel.example/notify", "", "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	kind, entry, err := session.CheckoutURL()
	if err != nil || kind != "qr" || entry != "https://cashier.example/qr" || session.TradeNo != "trade" {
		t.Fatalf("session = %#v, %v", session, err)
	}
	receipt, err := gateway.QueryOrder(context.Background(), "order", "trade")
	if err != nil || !receipt.Unpaid || receipt.Paid || receipt.Money != "10.29" {
		t.Fatalf("receipt = %#v, %v", receipt, err)
	}
	if err := gateway.ValidatePayment("Plan", "order", "10.29", "", "", "203.0.113.9"); err == nil {
		t.Fatal("missing notify URL accepted")
	}
	if requests != 2 {
		t.Fatalf("made %d requests", requests)
	}
}

func TestV1AmbiguousCreateIsRejected(t *testing.T) {
	for _, body := range []string{`{"code":1,"trade_no":"trade","payurl":"https://pay.example","qrcode":"qr"}`, `{"code":1,"qrcode":"qr"}`, `{"code":1,"code":0,"trade_no":"trade","qrcode":"qr"}`, `{"code":1,"trade_no":"trade","payurl":{"url":"https://pay.example"}}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.Write([]byte(body)) }))
			defer server.Close()
			gateway, err := Open(GatewayConfig{Endpoint: server.URL, CheckoutMode: "api", MerchantID: "1001", Key: "key", PaymentType: "alipay"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := gateway.CreatePayment(context.Background(), "Plan", "order", "10.29", "https://panel.example/notify", "", "203.0.113.9"); err == nil {
				t.Fatal("ambiguous create accepted")
			}
		})
	}
}

func TestV2HTTPPaymentLifecycleAndIdentity(t *testing.T) {
	gateway, platform := testV2Gateway(t)
	gateway.config.CheckoutMode, gateway.config.PaymentType = "api", "alipay"
	wrongOrder := false
	unsignedClose := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		params := make(map[string]string)
		for name := range request.PostForm {
			params[name] = request.PostForm.Get(name)
		}
		verifier := *gateway
		verifier.publicKey = &gateway.privateKey.PublicKey
		if err := verifier.verifyV2(params); err != nil {
			t.Errorf("request signature: %v", err)
		}
		response := map[string]string{"code": "0", "pid": "1001", "timestamp": strconv.FormatInt(time.Now().Unix(), 10), "sign_type": "RSA"}
		switch request.URL.Path {
		case "/shop/api/pay/create":
			if params["method"] != "jump" || params["money"] != "10.29" || params["out_trade_no"] != "order" || params["clientip"] != "203.0.113.9" {
				t.Errorf("unexpected V2 create: %v", params)
			}
			response["trade_no"], response["pay_type"], response["pay_info"] = "trade", "jump", "https://cashier.example/pay"
		case "/shop/api/pay/query":
			response["trade_no"], response["out_trade_no"], response["type"], response["money"], response["status"] = "trade", "order", "alipay", "10.29", "0"
			if wrongOrder {
				response["out_trade_no"] = "another-order"
			}
		case "/shop/api/pay/close":
			if params["trade_no"] != "trade" || params["out_trade_no"] != "" {
				t.Errorf("unexpected V2 close: %v", params)
			}
			if unsignedClose {
				writer.Write([]byte(`{"code":0}`))
				return
			}
		default:
			t.Errorf("unexpected path %s", request.URL.Path)
			http.NotFound(writer, request)
			return
		}
		signature, err := signV2(platform, response)
		if err != nil {
			t.Error(err)
			return
		}
		wire := make(map[string]any, len(response)+1)
		for name, value := range response {
			wire[name] = value
		}
		wire["code"], wire["sign"] = 0, signature
		json.NewEncoder(writer).Encode(wire)
	}))
	defer server.Close()
	gateway.endpoint, gateway.http = server.URL+"/shop", server.Client()
	session, err := gateway.CreatePayment(context.Background(), "Plan", "order", "10.29", "https://panel.example/notify", "", "203.0.113.9")
	if err != nil || session == nil || session.TradeNo != "trade" {
		t.Fatalf("session = %#v, %v", session, err)
	}
	receipt, err := gateway.QueryOrder(context.Background(), "order", "trade")
	if err != nil || receipt == nil || !receipt.Unpaid {
		t.Fatalf("receipt = %#v, %v", receipt, err)
	}
	if err := gateway.CloseOrder(context.Background(), "order", "trade"); err != nil {
		t.Fatal(err)
	}
	wrongOrder = true
	if _, err := gateway.QueryOrder(context.Background(), "order", "trade"); err == nil {
		t.Fatal("signed response for another order accepted")
	}
	unsignedClose = true
	if err := gateway.CloseOrder(context.Background(), "order", "trade"); err == nil {
		t.Fatal("unsigned successful close accepted")
	}
}

func TestGatewayNotificationsAndPageSignatures(t *testing.T) {
	for _, version := range []string{"v1", "v2"} {
		t.Run(version, func(t *testing.T) {
			gateway, platform := testV2Gateway(t)
			if version == "v1" {
				var err error
				gateway, err = Open(GatewayConfig{Endpoint: "https://pay.example/shop", MerchantID: "1001", Key: "shared-key", PaymentType: "alipay"})
				if err != nil {
					t.Fatal(err)
				}
			}
			page, err := gateway.PagePayment("Plan", "order", "10.29", "https://panel.example/notify", "https://panel.example/orders")
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := url.Parse(page)
			if err != nil {
				t.Fatal(err)
			}
			if version == "v2" && parsed.Path != "/shop/api/pay/submit" {
				t.Fatalf("V2 page = %s", parsed.Path)
			}
			params := map[string]string{"pid": "1001", "trade_no": "trade", "out_trade_no": "order", "money": "10.29", "type": "alipay", "trade_status": "TRADE_SUCCESS"}
			if version == "v1" {
				params["sign_type"], params["sign"] = "MD5", gateway.legacy.createSign(params)
			} else {
				params["sign_type"], params["timestamp"] = "RSA", strconv.FormatInt(time.Now().Unix(), 10)
				params["sign"], err = signV2(platform, params)
				if err != nil {
					t.Fatal(err)
				}
			}
			values := make(url.Values, len(params))
			for name, value := range params {
				values.Set(name, value)
			}
			if _, err := gateway.VerifyNotification(values); err != nil {
				t.Fatal(err)
			}
			values.Add("money", "0.01")
			if _, err := gateway.VerifyNotification(values); err == nil {
				t.Fatal("duplicate notification amount accepted")
			}
			values.Set("money", "10.28")
			if _, err := gateway.VerifyNotification(values); err == nil {
				t.Fatal("tampered notification amount accepted")
			}
		})
	}
}
