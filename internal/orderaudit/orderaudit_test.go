package orderaudit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/pkg/requestmeta"
)

type fakeLogInserter struct {
	inserted []*log.SystemLog
	err      error
}

func (f *fakeLogInserter) Insert(_ context.Context, data *log.SystemLog) error {
	if f.err != nil {
		return f.err
	}
	f.inserted = append(f.inserted, data)
	return nil
}

func sampleOrder() *order.Order {
	return &order.Order{
		Id:             1,
		UserId:         99,
		OrderNo:        "202601010001",
		Type:           1,
		Quantity:       2,
		Price:          1000,
		Amount:         900,
		GiftAmount:     0,
		Discount:       0,
		CouponDiscount: 100,
		PaymentId:      7,
		Method:         "alipay",
		FeeAmount:      5,
		SubscribeId:    42,
	}
}

func TestInsertCreatedPersistsAuditRow(t *testing.T) {
	repo := &fakeLogInserter{}
	data := sampleOrder()

	ctx := requestmeta.With(context.Background(), requestmeta.New("203.0.113.9", "agent/1.0"))
	if err := InsertCreated(ctx, repo, data, SourceUser); err != nil {
		t.Fatalf("InsertCreated: %v", err)
	}
	if len(repo.inserted) != 1 {
		t.Fatalf("expected exactly one audit row, got %d", len(repo.inserted))
	}

	row := repo.inserted[0]
	if row.Type != log.TypeOrderCreated.Uint8() {
		t.Fatalf("row type = %d, want %d", row.Type, log.TypeOrderCreated.Uint8())
	}
	if row.ObjectID != data.UserId {
		t.Fatalf("row object id = %d, want %d", row.ObjectID, data.UserId)
	}
	if row.Date == "" {
		t.Fatal("row date must be set")
	}

	var content log.OrderCreated
	if err := content.Unmarshal([]byte(row.Content)); err != nil {
		t.Fatalf("unmarshal stored content: %v", err)
	}
	if content.OrderNo != data.OrderNo || content.Source != SourceUser {
		t.Fatalf("unexpected stored content: %+v", content)
	}
	if content.CouponDiscount != 100 || content.PaymentID != 7 || content.SubscribeID != 42 {
		t.Fatalf("stored content lost fields: %+v", content)
	}
	if content.ClientIP != "203.0.113.9" || content.UserAgent != "agent/1.0" {
		t.Fatalf("request metadata not persisted: %+v", content.Metadata)
	}
	if content.Timestamp <= 0 {
		t.Fatalf("timestamp must be set: %d", content.Timestamp)
	}

	// The serialized audit payload must not include the coupon code itself.
	var raw map[string]any
	if err := json.Unmarshal([]byte(row.Content), &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["coupon"]; ok {
		t.Fatal("audit payload must not contain the coupon code")
	}
}

func TestInsertCreatedWithoutMetadataStillWorks(t *testing.T) {
	repo := &fakeLogInserter{}
	if err := InsertCreated(context.Background(), repo, sampleOrder(), SourceGuest); err != nil {
		t.Fatalf("InsertCreated: %v", err)
	}
	var content log.OrderCreated
	if err := content.Unmarshal([]byte(repo.inserted[0].Content)); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if content.Source != SourceGuest {
		t.Fatalf("source = %q, want %q", content.Source, SourceGuest)
	}
	if content.ClientIP != "" {
		t.Fatalf("expected empty client ip, got %q", content.ClientIP)
	}
}

func TestInsertCreatedRejectsInvalidArguments(t *testing.T) {
	repo := &fakeLogInserter{}
	if err := InsertCreated(context.Background(), nil, sampleOrder(), SourceUser); err == nil {
		t.Fatal("nil log repository must error")
	}
	if err := InsertCreated(context.Background(), repo, nil, SourceUser); err == nil {
		t.Fatal("nil order data must error")
	}
	if len(repo.inserted) != 0 {
		t.Fatal("no rows should be inserted on invalid arguments")
	}
}

func TestInsertCreatedPropagatesRepositoryError(t *testing.T) {
	repo := &fakeLogInserter{err: errors.New("db down")}
	if err := InsertCreated(context.Background(), repo, sampleOrder(), SourceUser); err == nil {
		t.Fatal("expected repository error to propagate")
	}
}
