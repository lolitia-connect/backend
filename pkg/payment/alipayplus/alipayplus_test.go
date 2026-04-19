package alipayplus

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alipay/global-open-sdk-go/com/alipay/api/tools"
)

// 填入真实 Alipay+ 配置后，可直接执行 TestPreCreateTrade 请求对应网关。
const (
	integrationGatewayURL       = ""
	integrationClientID         = ""
	integrationMerchantID       = ""
	integrationPrivateKey       = ""
	integrationAlipayPublicKey  = ""
	integrationCurrency         = ""
	integrationPaymentMethod    = "" // 例如 ALIPAY_HK / ALIPAY_CN
	integrationNotifyURL        = ""
	integrationRedirectURL      = ""
	integrationInvoiceName      = "Perfect Panel"
	integrationReferenceBuyerID = ""
)

func TestPreCreateTrade(t *testing.T) {
	client := NewClient(loadIntegrationConfig(t))
	orderNo := fmt.Sprintf("ORDER_%d", time.Now().UnixNano())

	payload, err := client.PreCreateTrade(context.Background(), Order{
		OrderNo:          orderNo,
		Amount:           1234,
		ReferenceBuyerId: integrationReferenceBuyerID,
	})
	if err != nil {
		t.Fatalf("PreCreateTrade returned error: %v", err)
	}
	if strings.TrimSpace(payload) == "" {
		t.Fatal("expected non-empty payload")
	}
}

func TestPreCreateTrade_RequestFollowsCashierDocs(t *testing.T) {
	privateKey, publicKey := generateTestKeys(t)
	const (
		clientID       = "test-client-id"
		merchantID     = "test-merchant-id"
		orderNo        = "ORDER_20260417_DOCS"
		notifyURL      = "https://example.com/notify"
		redirectURL    = "https://example.com/payment/return"
		expectedNormal = "https://cashier.alipayplus.test/checkout/session"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/ams/api/v1/payments/pay" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Client-Id") != clientID {
			t.Fatalf("unexpected client id: %s", r.Header.Get("Client-Id"))
		}
		if r.Header.Get("Signature") == "" {
			t.Fatal("missing request signature")
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}

		var payReq map[string]any
		if err := json.Unmarshal(body, &payReq); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if payReq["paymentRequestId"] != orderNo {
			t.Fatalf("unexpected paymentRequestId: %v", payReq["paymentRequestId"])
		}
		if payReq["paymentNotifyUrl"] != notifyURL {
			t.Fatalf("unexpected notify url: %v", payReq["paymentNotifyUrl"])
		}
		if payReq["paymentRedirectUrl"] != redirectURL {
			t.Fatalf("unexpected redirect url: %v", payReq["paymentRedirectUrl"])
		}
		if _, ok := payReq["productCode"]; ok {
		}
		if payReq["productCode"] != "CASHIER_PAYMENT" {
			t.Fatalf("unexpected productCode: %+v", payReq["productCode"])
		}
		env, ok := payReq["env"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected top-level env payload: %+v", payReq["env"])
		}
		if env["terminalType"] != "WEB" {
			t.Fatalf("unexpected top-level terminalType: %+v", env)
		}

		paymentAmount, ok := payReq["paymentAmount"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected paymentAmount payload: %+v", payReq["paymentAmount"])
		}
		if paymentAmount["value"] != "1234" || paymentAmount["currency"] != "HKD" {
			t.Fatalf("unexpected paymentAmount: %+v", paymentAmount)
		}

		paymentMethod, ok := payReq["paymentMethod"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected payment method payload: %+v", payReq["paymentMethod"])
		}
		if paymentMethod["paymentMethodType"] != "ALIPAY_HK" {
			t.Fatalf("unexpected payment method type: %+v", paymentMethod)
		}

		if _, ok := payReq["paymentFactor"]; ok {
			t.Fatalf("paymentFactor should not be sent in sdk/demo style request: %+v", payReq["paymentFactor"])
		}

		settlementStrategy, ok := payReq["settlementStrategy"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected settlement strategy payload: %+v", payReq["settlementStrategy"])
		}
		if settlementStrategy["settlementCurrency"] != "HKD" {
			t.Fatalf("unexpected settlementStrategy: %+v", settlementStrategy)
		}

		order, ok := payReq["order"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected order payload: %+v", payReq["order"])
		}
		if order["referenceOrderId"] != orderNo {
			t.Fatalf("unexpected referenceOrderId: %+v", order)
		}
		orderEnv, ok := order["env"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected env payload: %+v", order["env"])
		}
		if orderEnv["terminalType"] != "WEB" {
			t.Fatalf("unexpected terminalType: %+v", orderEnv)
		}
		if _, exists := orderEnv["osType"]; exists {
			t.Fatalf("osType should be omitted for WEB terminalType: %+v", orderEnv)
		}

		respBody, err := json.Marshal(map[string]any{
			"result": map[string]any{
				"resultCode":    "PAYMENT_IN_PROCESS",
				"resultStatus":  "U",
				"resultMessage": "Payment in process",
			},
			"paymentRequestId": orderNo,
			"normalUrl":        expectedNormal,
		})
		if err != nil {
			t.Fatalf("marshal response: %v", err)
		}

		responseTime := "1710000000000"
		sign, err := tools.GenSign(http.MethodPost, r.URL.Path, clientID, responseTime, string(respBody), privateKey)
		if err != nil {
			t.Fatalf("sign response: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Client-Id", clientID)
		w.Header().Set("response-time", responseTime)
		w.Header().Set("Signature", "algorithm=RSA256,keyVersion=1,signature="+sign)
		if _, err := w.Write(respBody); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	defer server.Close()

	client := NewClient(Config{
		ClientId:        clientID,
		MerchantId:      merchantID,
		PrivateKey:      privateKey,
		AlipayPublicKey: publicKey,
		GatewayUrl:      server.URL,
		Currency:        "HKD",
		PaymentMethod:   "ALIPAY_HK",
		InvoiceName:     "Perfect Panel",
		NotifyURL:       notifyURL,
		RedirectURL:     redirectURL,
	})

	payload, err := client.PreCreateTrade(context.Background(), Order{
		OrderNo:          orderNo,
		Amount:           1234,
		ReferenceBuyerId: "user-1",
	})
	if err != nil {
		t.Fatalf("PreCreateTrade returned error: %v", err)
	}
	if payload != expectedNormal {
		t.Fatalf("unexpected payload: %s", payload)
	}
}

func loadIntegrationConfig(t *testing.T) Config {
	t.Helper()

	cfg := Config{
		ClientId:        integrationClientID,
		MerchantId:      integrationMerchantID,
		PrivateKey:      integrationPrivateKey,
		AlipayPublicKey: integrationAlipayPublicKey,
		GatewayUrl:      integrationGatewayURL,
		Currency:        integrationCurrency,
		PaymentMethod:   integrationPaymentMethod,
		InvoiceName:     integrationInvoiceName,
		NotifyURL:       integrationNotifyURL,
		RedirectURL:     integrationRedirectURL,
	}

	missing := make([]string, 0, 8)
	if strings.TrimSpace(cfg.GatewayUrl) == "" {
		missing = append(missing, "integrationGatewayURL")
	}
	if strings.TrimSpace(cfg.ClientId) == "" {
		missing = append(missing, "integrationClientID")
	}
	if strings.TrimSpace(cfg.MerchantId) == "" {
		missing = append(missing, "integrationMerchantID")
	}
	if strings.TrimSpace(cfg.PrivateKey) == "" {
		missing = append(missing, "integrationPrivateKey")
	}
	if strings.TrimSpace(cfg.AlipayPublicKey) == "" {
		missing = append(missing, "integrationAlipayPublicKey")
	}
	if strings.TrimSpace(cfg.Currency) == "" {
		missing = append(missing, "integrationCurrency")
	}
	if strings.TrimSpace(cfg.PaymentMethod) == "" {
		missing = append(missing, "integrationPaymentMethod")
	}
	if strings.TrimSpace(cfg.NotifyURL) == "" {
		missing = append(missing, "integrationNotifyURL")
	}
	if strings.TrimSpace(cfg.RedirectURL) == "" {
		missing = append(missing, "integrationRedirectURL")
	}
	if len(missing) > 0 {
		t.Skipf("fill AlipayPlus integration test config first: %s", strings.Join(missing, ", "))
	}

	return cfg
}

func TestPreCreateTrade_RequireConfiguredPaymentMethod(t *testing.T) {
	privateKey, publicKey := generateTestKeys(t)

	client := NewClient(Config{
		ClientId:        "test-client-id",
		MerchantId:      "test-merchant-id",
		PrivateKey:      privateKey,
		AlipayPublicKey: publicKey,
		GatewayUrl:      "https://example.com",
		Currency:        "CNY",
		InvoiceName:     "Perfect Panel",
		NotifyURL:       "https://example.com/notify",
		RedirectURL:     "https://example.com/payment/return",
	})

	_, err := client.PreCreateTrade(context.Background(), Order{
		OrderNo: "ORDER_20260417_002",
		Amount:  2000,
	})
	if err == nil {
		t.Fatal("expected error when payment method is not configured")
	}
	if err.Error() != "paymentMethod is empty" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreCreateTrade_RequireConfiguredCurrency(t *testing.T) {
	privateKey, publicKey := generateTestKeys(t)
	client := NewClient(Config{
		ClientId:        "test-client-id",
		MerchantId:      "test-merchant-id",
		PrivateKey:      privateKey,
		AlipayPublicKey: publicKey,
		GatewayUrl:      "https://example.com",
		PaymentMethod:   "ALIPAY_CN",
		InvoiceName:     "Perfect Panel",
		NotifyURL:       "https://example.com/notify",
		RedirectURL:     "https://example.com/payment/return",
	})

	_, err := client.PreCreateTrade(context.Background(), Order{
		OrderNo: "ORDER_20260417_003",
		Amount:  2000,
	})
	if err == nil {
		t.Fatal("expected error when currency is not configured")
	}
	if err.Error() != "currency is empty" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeNotification_PaymentPendingTreatedAsPending(t *testing.T) {
	privateKey, publicKey := generateTestKeys(t)
	const (
		clientID    = "test-client-id"
		orderNo     = "ORDER_20260417_PENDING"
		requestTime = "1710000000003"
	)

	body, err := json.Marshal(map[string]any{
		"notifyType": "PAYMENT_PENDING",
		"result": map[string]any{
			"resultCode":    "SUCCESS",
			"resultStatus":  "S",
			"resultMessage": "pending",
		},
		"paymentRequestId": orderNo,
		"paymentAmount": map[string]any{
			"value":    "1234",
			"currency": "CNY",
		},
	})
	if err != nil {
		t.Fatalf("marshal notify body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "https://example.com/v1/notify/AlipayPlus/token", strings.NewReader(string(body)))
	sign, err := tools.GenSign(http.MethodPost, req.URL.Path, clientID, requestTime, string(body), privateKey)
	if err != nil {
		t.Fatalf("sign notify body: %v", err)
	}
	req.Header.Set("Client-Id", clientID)
	req.Header.Set("Request-Time", requestTime)
	req.Header.Set("Signature", "algorithm=RSA256,keyVersion=1,signature="+sign)

	client := NewClient(Config{ClientId: clientID, PrivateKey: privateKey, AlipayPublicKey: publicKey})
	notify, err := client.DecodeNotification(req)
	if err != nil {
		t.Fatalf("DecodeNotification returned error: %v", err)
	}
	if notify.Status != Pending {
		t.Fatalf("unexpected status: %s", notify.Status)
	}
}

func TestDecodeNotification(t *testing.T) {
	privateKey, publicKey := generateTestKeys(t)
	const (
		clientID    = "test-client-id"
		orderNo     = "ORDER_20260417_NOTIFY"
		requestTime = "1710000000001"
	)

	body, err := json.Marshal(map[string]any{
		"notifyType": "PAYMENT_RESULT",
		"result": map[string]any{
			"resultCode":    "SUCCESS",
			"resultStatus":  "S",
			"resultMessage": "Success",
		},
		"paymentRequestId": orderNo,
		"paymentAmount": map[string]any{
			"value":    "12.34",
			"currency": "USD",
		},
	})
	if err != nil {
		t.Fatalf("marshal notify body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "https://example.com/v1/notify/AlipayPlus/token", strings.NewReader(string(body)))
	sign, err := tools.GenSign(http.MethodPost, req.URL.Path, clientID, requestTime, string(body), privateKey)
	if err != nil {
		t.Fatalf("sign notify body: %v", err)
	}
	req.Header.Set("Client-Id", clientID)
	req.Header.Set("Request-Time", requestTime)
	req.Header.Set("Signature", "algorithm=RSA256,keyVersion=1,signature="+sign)

	client := NewClient(Config{
		ClientId:        clientID,
		PrivateKey:      privateKey,
		AlipayPublicKey: publicKey,
	})

	notify, err := client.DecodeNotification(req)
	if err != nil {
		t.Fatalf("DecodeNotification returned error: %v", err)
	}
	if notify.OrderNo != orderNo {
		t.Fatalf("unexpected order no: %s", notify.OrderNo)
	}
	if notify.Amount != 1234 {
		t.Fatalf("unexpected amount: %d", notify.Amount)
	}
	if notify.Status != Success {
		t.Fatalf("unexpected status: %s", notify.Status)
	}
}

func generateTestKeys(t *testing.T) (privateKey string, publicKey string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}

	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}

	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	return base64.StdEncoding.EncodeToString(privateDER), base64.StdEncoding.EncodeToString(publicDER)
}
