package portal

import (
	"context"
	"testing"

	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/pkg/constant"
)

func TestOrdinaryPaidOrderDoesNotIssueGuestSession(t *testing.T) {
	logic := NewQueryPurchaseOrderLogic(context.Background(), nil)
	for _, status := range []uint8{order.StatusPaid, order.StatusFinished} {
		info := &order.Order{UserId: 42, Status: status}
		token, err := logic.handleTemporaryOrder(info, "")
		if err != nil || token != "" {
			t.Fatalf("ordinary order returned guest session: token=%q err=%v", token, err)
		}
	}
}

func TestGuestSessionRejectsWrongCapabilityBeforeStorage(t *testing.T) {
	logic := NewQueryPurchaseOrderLogic(context.Background(), nil)
	info := &order.Order{UserId: 42, GuestCheckoutTokenHash: constant.CheckoutTokenHash("owner-capability")}
	if _, err := logic.handleTemporaryOrder(info, "other-capability"); err == nil {
		t.Fatal("guest session accepted a different checkout capability")
	}
}
