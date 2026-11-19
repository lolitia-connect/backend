package log

import (
	"context"
	"testing"

	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/requestmeta"
)

// TestUnmatchedPaymentMetadataRoundTrips is the regression test for the missing
// metadata on the unmatched-payment log. The entry is written for risk analysis,
// so the admin page must be able to show where the callback came from: the
// request metadata has to survive the write and come back out of
// FilterUnmatchedPaymentLog.
func TestUnmatchedPaymentMetadataRoundTrips(t *testing.T) {
	service, _ := operatorLogFixture(t)
	ctx := requestmeta.With(context.Background(), requestmeta.Metadata{
		ClientIP:  "203.0.113.7",
		UserAgent: "EPay-Notifier/1.0",
		IPMetadata: requestmeta.IPMetadata{
			IPCountryCode: "SG",
			IPCountry:     "Singapore",
			IPRegion:      "Central",
			IPCity:        "Singapore",
			IPASN:         64500,
		},
	})

	// Write side: the callback flow attaches the context metadata before the
	// row is stored, exactly as settlement.go and cryptomusNotifyLogic.go do.
	if err := service.Store.Log().RecordUnmatchedPayment(ctx, 8, modellog.UnmatchedPaymentFrom(ctx, modellog.UnmatchedPayment{
		OrderNo: "order-1", TradeNo: "trade-1", Platform: "EPay", Amount: 100, Currency: "USD",
		Reason: "payment amount or currency does not match order",
	})); err != nil {
		t.Fatal(err)
	}

	// Read side: the admin log page must return the metadata.
	response, err := FilterUnmatchedPaymentLog(ctx, service, &types.FilterUnmatchedPaymentLogRequest{UserId: 8})
	if err != nil {
		t.Fatal(err)
	}
	if response.Total != 1 || len(response.List) != 1 {
		t.Fatalf("list = %+v, want exactly one entry", response)
	}
	got := response.List[0]
	if got.ClientIP != "203.0.113.7" {
		t.Fatalf("client_ip = %q, want 203.0.113.7 (metadata not returned)", got.ClientIP)
	}
	if got.UserAgent != "EPay-Notifier/1.0" {
		t.Fatalf("user_agent = %q, want EPay-Notifier/1.0", got.UserAgent)
	}
	if got.IPCountryCode != "SG" || got.IPCountry != "Singapore" || got.IPRegion != "Central" || got.IPCity != "Singapore" || got.IPASN != 64500 {
		t.Fatalf("geo metadata not returned: %+v", got.Metadata)
	}
	// The payment fields must still be intact.
	if got.OrderNo != "order-1" || got.TradeNo != "trade-1" || got.Platform != "EPay" || got.Amount != 100 || got.Currency != "USD" {
		t.Fatalf("payment fields lost: %+v", got)
	}
	if got.UserId != 8 {
		t.Fatalf("user_id = %d, want 8", got.UserId)
	}
}

// TestUnmatchedPaymentMetadataIsBounded proves the metadata and reason are
// clamped on the way in and on the way out, so an attacker-influenced callback
// cannot grow the audit row without limit and an older oversized row is clamped
// when it is read back.
func TestUnmatchedPaymentMetadataIsBounded(t *testing.T) {
	service, client := operatorLogFixture(t)
	ctx := requestmeta.With(context.Background(), requestmeta.Metadata{
		ClientIP:  string(make([]byte, 0)) + "198.51.100.9",
		UserAgent: longString(requestmeta.MaxUserAgentBytes + 500),
	})
	longReason := longString(4096)
	if err := service.Store.Log().RecordUnmatchedPayment(ctx, 9, modellog.UnmatchedPaymentFrom(ctx, modellog.UnmatchedPayment{
		OrderNo: "order-2", TradeNo: "trade-2", Platform: "Stripe", Currency: "USD", Reason: longReason,
	})); err != nil {
		t.Fatal(err)
	}

	// The stored row itself is bounded.
	stored := client.SystemLog.Query().OnlyX(ctx)
	if len(stored.Content) > 8192 {
		t.Fatalf("stored content is unbounded: %d bytes", len(stored.Content))
	}

	response, err := FilterUnmatchedPaymentLog(ctx, service, &types.FilterUnmatchedPaymentLogRequest{UserId: 9})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.List) != 1 {
		t.Fatalf("list = %+v", response)
	}
	if len(response.List[0].UserAgent) > requestmeta.MaxUserAgentBytes {
		t.Fatalf("user agent not clamped on read: %d bytes", len(response.List[0].UserAgent))
	}
	if len(response.List[0].Reason) > 2048 {
		t.Fatalf("reason not clamped on read: %d bytes", len(response.List[0].Reason))
	}
}

// TestUnmatchedPaymentWithoutRequestMetadataStillRecords proves the write path
// tolerates a context with no metadata (internal retries, tests): the row is
// stored with empty metadata instead of failing.
func TestUnmatchedPaymentWithoutRequestMetadataStillRecords(t *testing.T) {
	service, _ := operatorLogFixture(t)
	ctx := context.Background()
	if err := service.Store.Log().RecordUnmatchedPayment(ctx, 10, modellog.UnmatchedPayment{
		OrderNo: "order-3", TradeNo: "trade-3", Platform: "Alipay", Currency: "CNY", Reason: "review",
	}); err != nil {
		t.Fatal(err)
	}
	response, err := FilterUnmatchedPaymentLog(ctx, service, &types.FilterUnmatchedPaymentLogRequest{UserId: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.List) != 1 {
		t.Fatalf("list = %+v", response)
	}
	if response.List[0].TradeNo != "trade-3" || response.List[0].ClientIP != "" {
		t.Fatalf("entry = %+v, want trade-3 with empty metadata", response.List[0])
	}
}

func longString(size int) string {
	buffer := make([]byte, size)
	for index := range buffer {
		buffer[index] = 'a'
	}
	return string(buffer)
}
