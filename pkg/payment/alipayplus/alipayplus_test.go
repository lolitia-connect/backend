package alipayplus

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	requestPay "github.com/alipay/global-open-sdk-go/com/alipay/api/request/pay"
	"github.com/alipay/global-open-sdk-go/com/alipay/api/tools"
)

func TestPreCreateTrade(t *testing.T) {
	privateKey, publicKey := generateTestKeys(t)
	const (
		clientID       = "test-client-id"
		merchantID     = "test-merchant-id"
		orderNo        = "ORDER_20260417_001"
		notifyURL      = "https://example.com/notify"
		redirectURL    = "https://example.com/payment/return"
		expectedURL    = "https://cashier.alipayplus.test/checkout/session"
		expectedAmount = "12.34"
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

		var payReq requestPay.AlipayPayRequest
		if err := json.Unmarshal(body, &payReq); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if payReq.PaymentRequestId != orderNo {
			t.Fatalf("unexpected paymentRequestId: %s", payReq.PaymentRequestId)
		}
		if payReq.PaymentNotifyUrl != notifyURL {
			t.Fatalf("unexpected notify url: %s", payReq.PaymentNotifyUrl)
		}
		if payReq.PaymentRedirectUrl != redirectURL {
			t.Fatalf("unexpected redirect url: %s", payReq.PaymentRedirectUrl)
		}
		if payReq.Order == nil || payReq.Order.Merchant == nil {
			t.Fatal("missing order merchant info")
		}
		if payReq.Order.ReferenceOrderId != orderNo {
			t.Fatalf("unexpected referenceOrderId: %s", payReq.Order.ReferenceOrderId)
		}
		if payReq.Order.Merchant.ReferenceMerchantId != merchantID {
			t.Fatalf("unexpected merchant id: %s", payReq.Order.Merchant.ReferenceMerchantId)
		}
		if payReq.PaymentAmount == nil || payReq.PaymentAmount.Value != expectedAmount || payReq.PaymentAmount.Currency != "USD" {
			t.Fatalf("unexpected payment amount: %+v", payReq.PaymentAmount)
		}
		if payReq.ProductCode != "CASHIER_PAYMENT" {
			t.Fatalf("unexpected product code: %s", payReq.ProductCode)
		}
		if payReq.Order.Env == nil || string(payReq.Order.Env.TerminalType) != "WEB" {
			t.Fatalf("unexpected env: %+v", payReq.Order.Env)
		}

		respBody, err := json.Marshal(map[string]any{
			"result": map[string]any{
				"resultCode":    "SUCCESS",
				"resultStatus":  "S",
				"resultMessage": "Success",
			},
			"paymentRequestId": orderNo,
			"normalUrl": expectedURL,
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
		Currency:        "USD",
		InvoiceName:     "Perfect Panel",
		NotifyURL:       notifyURL,
		RedirectURL:     redirectURL,
	})

	payload, err := client.PreCreateTrade(context.Background(), Order{
		OrderNo: orderNo,
		Amount:  1234,
	})
	if err != nil {
		t.Fatalf("PreCreateTrade returned error: %v", err)
	}
	if payload != expectedURL {
		t.Fatalf("unexpected payload: %s", payload)
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
