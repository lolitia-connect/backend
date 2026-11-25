package alipay

import (
	"context"
	"testing"
	"time"

	alipaySDK "github.com/smartwalle/alipay/v3"
)

func TestAlipayClientRejectsInvalidConfigurationAndExpiredOrders(t *testing.T) {
	var nilClient *Client
	if _, err := nilClient.PreCreateTrade(context.Background(), Order{}); err == nil {
		t.Fatal("nil Alipay client accepted precreate")
	}
	if _, err := nilClient.QueryPayment(context.Background(), "order"); err == nil {
		t.Fatal("nil Alipay client accepted query")
	}
	if err := nilClient.CloseTrade(context.Background(), "order"); err == nil {
		t.Fatal("nil Alipay client accepted close")
	}
	if _, err := nilClient.DecodeNotification(context.Background(), nil); err == nil {
		t.Fatal("nil Alipay client accepted notification")
	}
	client := &Client{client: &alipaySDK.Client{}}
	if _, err := client.PreCreateTrade(context.Background(), Order{OrderNo: "expired", Amount: 100, ExpiresAt: time.Now().Add(-time.Minute)}); err == nil {
		t.Fatal("expired Alipay order was accepted")
	}
	if _, err := NewClient(Config{}).PreCreateTrade(context.Background(), Order{OrderNo: "invalid", Amount: 100}); err == nil {
		t.Fatal("invalid Alipay configuration was accepted")
	}
}
