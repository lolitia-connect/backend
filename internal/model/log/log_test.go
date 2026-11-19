package log

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	_ "modernc.org/sqlite"
)

func TestTypeOrderCreatedValue(t *testing.T) {
	if TypeOrderCreated.Uint8() != 35 {
		t.Fatalf("TypeOrderCreated.Uint8() = %d, want 35", TypeOrderCreated.Uint8())
	}
	if TypeOrderCreated.Uint8() == TypeGift.Uint8() {
		t.Fatal("TypeOrderCreated must not collide with TypeGift")
	}
}

func TestOrderCreatedMarshalRoundTrip(t *testing.T) {
	original := &OrderCreated{
		OrderNo:        "202601010001",
		OrderType:      1,
		Quantity:       2,
		Price:          1000,
		Amount:         950,
		GiftAmount:     50,
		Discount:       0,
		CouponDiscount: 100,
		PaymentID:      7,
		Method:         "alipay",
		FeeAmount:      10,
		SubscribeID:    42,
		Source:         "user",
		Timestamp:      1767225600000,
	}
	original.ClientIP = "203.0.113.1"
	original.UserAgent = "Mozilla/5.0"

	data, err := original.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// The audit entry must never leak sensitive values.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for _, forbidden := range []string{"coupon", "trade_no", "subscribe_token", "password"} {
		if _, ok := raw[forbidden]; ok {
			t.Fatalf("audit payload must not contain %q", forbidden)
		}
	}

	var decoded OrderCreated
	if err := decoded.Unmarshal(data); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.OrderNo != original.OrderNo ||
		decoded.OrderType != original.OrderType ||
		decoded.Quantity != original.Quantity ||
		decoded.Price != original.Price ||
		decoded.Amount != original.Amount ||
		decoded.GiftAmount != original.GiftAmount ||
		decoded.CouponDiscount != original.CouponDiscount ||
		decoded.PaymentID != original.PaymentID ||
		decoded.Method != original.Method ||
		decoded.FeeAmount != original.FeeAmount ||
		decoded.SubscribeID != original.SubscribeID ||
		decoded.Source != original.Source ||
		decoded.Timestamp != original.Timestamp {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", decoded, original)
	}
	if decoded.ClientIP != "203.0.113.1" || decoded.UserAgent != "Mozilla/5.0" {
		t.Fatalf("request metadata not preserved: %+v", decoded.Metadata)
	}
}

func TestOrderCreatedUnmarshalRejectsGarbage(t *testing.T) {
	var decoded OrderCreated
	if err := decoded.Unmarshal([]byte("not-json")); err == nil {
		t.Fatal("expected error for non-JSON content")
	}
}

func TestJSONLogFiltersMatchExactIDs(t *testing.T) {
	database, err := sql.Open("sqlite", "file:log-exact-ids?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	defer client.Close()
	ctx := context.Background()
	for _, content := range []string{`{"user_subscribe_id":12,"user_id":7}`, `{"user_subscribe_id":120,"user_id":7}`, `{"user_subscribe_id":12,"user_id":8}`} {
		client.SystemLog.Create().SetType(TypeSubscribe.Uint8()).SetContent(content).SaveX(ctx)
	}
	rows, total, err := NewModel(client).FilterSystemLog(ctx, &FilterParams{Type: TypeSubscribe.Uint8(), ContentInt64: map[string]int64{"user_subscribe_id": 12, "user_id": 7}})
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("exact log filter = %+v, %d, %v", rows, total, err)
	}
}
