package alipayplus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alipay/global-open-sdk-go/com/alipay/api/model"
	responsePay "github.com/alipay/global-open-sdk-go/com/alipay/api/response/pay"
	"github.com/alipay/global-open-sdk-go/com/alipay/api/tools"
)

func TestQueryTradeMapsProviderStatusesAndFailures(t *testing.T) {
	privateKey, publicKey := generateTestKeys(t)
	status := "SUCCESS"
	resultStatus := "S"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ams/api/v1/payments/inquiryPayment" {
			t.Fatalf("unexpected query path %s", r.URL.Path)
		}
		body, err := json.Marshal(map[string]any{
			"result":           map[string]any{"resultCode": "SUCCESS", "resultStatus": resultStatus, "resultMessage": "fixture"},
			"paymentRequestId": "order-query", "paymentStatus": status,
		})
		if err != nil {
			t.Fatal(err)
		}
		requestTime := "1710000000010"
		sign, err := tools.GenSign(http.MethodPost, r.URL.Path, "client", requestTime, string(body), privateKey)
		if err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Client-Id", "client")
		w.Header().Set("response-time", requestTime)
		w.Header().Set("Signature", "algorithm=RSA256,keyVersion=1,signature="+sign)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client := NewClient(Config{ClientId: "client", PrivateKey: privateKey, AlipayPublicKey: publicKey, GatewayUrl: server.URL, Currency: "USD", PaymentMethod: "ALIPAY_HK"})
	for _, test := range []struct {
		name   string
		status string
		want   Status
	}{
		{"success", "SUCCESS", Success},
		{"processing", "PROCESSING", Pending},
		{"pending", "PENDING", Pending},
		{"failed", "FAIL", Failed},
		{"cancelled", "CANCELLED", Closed},
		{"unknown", "SOMETHING_ELSE", Error},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, resultStatus = test.status, "S"
			got, err := client.QueryTrade(context.Background(), "order-query")
			if err != nil || got != test.want {
				t.Fatalf("QueryTrade(%s) = %s, %v", test.status, got, err)
			}
		})
	}
	resultStatus = "F"
	if got, err := client.QueryTrade(context.Background(), "order-query"); err == nil || got != Error || !strings.Contains(err.Error(), "inquiry failed") {
		t.Fatalf("failed inquiry = %s, %v", got, err)
	}
}

func TestAlipayPlusPayloadAndAmountHelpers(t *testing.T) {
	for _, test := range []struct {
		name string
		resp *responsePay.AlipayPayResponse
		want string
	}{
		{"normal url", &responsePay.AlipayPayResponse{NormalUrl: "https://normal"}, "https://normal"},
		{"redirect form", &responsePay.AlipayPayResponse{RedirectActionForm: &model.RedirectActionForm{RedirectUrl: "https://redirect"}}, "https://redirect"},
		{"scheme", &responsePay.AlipayPayResponse{SchemeUrl: "scheme://pay"}, "scheme://pay"},
		{"app link", &responsePay.AlipayPayResponse{ApplinkUrl: "https://app"}, "https://app"},
		{"action redirect", &responsePay.AlipayPayResponse{PaymentActionForm: `{"redirectUrl":"https://action"}`}, "https://action"},
		{"action form", &responsePay.AlipayPayResponse{PaymentActionForm: "form-data"}, "form-data"},
		{"payment data", &responsePay.AlipayPayResponse{PaymentData: "payment-data"}, "payment-data"},
		{"code detail", &responsePay.AlipayPayResponse{OrderCodeForm: &model.OrderCodeForm{CodeDetails: []*model.CodeDetail{{CodeValue: "qr-value"}}}}, "qr-value"},
		{"empty", &responsePay.AlipayPayResponse{}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := resolvePaymentPayload(test.resp); got != test.want {
				t.Fatalf("resolvePaymentPayload() = %q, want %q", got, test.want)
			}
		})
	}
	for _, test := range []struct {
		value string
		want  int64
	}{
		{"12.34", 1234}, {"1234", 1234}, {"", 0},
	} {
		got, err := parseAmountToCent(&model.Amount{Value: test.value})
		if err != nil || got != test.want {
			t.Fatalf("parseAmountToCent(%q) = %d, %v", test.value, got, err)
		}
	}
	if _, err := parseAmountToCent(&model.Amount{Value: "bad"}); err == nil {
		t.Fatal("invalid payment amount was accepted")
	}
	if got := mapResultStatus(notifyTypePaymentPending, "S"); got != Pending || mapResultStatus("PAYMENT_RESULT", "U") != Pending || mapResultStatus("PAYMENT_RESULT", "F") != Failed || mapResultStatus("PAYMENT_RESULT", "X") != Error {
		t.Fatalf("mapped notification status = %q", got)
	}
	if normalized := normalizeConfig(Config{GatewayUrl: " https://gateway ", Currency: " usd ", PaymentMethod: " alipay_hk "}); normalized.GatewayUrl != "https://gateway" || normalized.Currency != "USD" || normalized.PaymentMethod != "ALIPAY_HK" {
		t.Fatalf("normalized config = %+v", normalized)
	}
}
