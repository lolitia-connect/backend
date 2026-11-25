package portal

import (
	"context"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/redis/go-redis/v9"
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

func TestPaidGuestCanSeeReceiptBeforeActivationCreatesAccount(t *testing.T) {
	logic := NewQueryPurchaseOrderLogic(context.Background(), nil)
	info := &order.Order{Status: order.StatusPaid, GuestCheckoutTokenHash: constant.CheckoutTokenHash("capability")}
	if token, err := logic.handleTemporaryOrder(info, "capability"); err != nil || token != "" {
		t.Fatalf("paid but unfulfilled guest got token=%q, err=%v", token, err)
	}
}

func TestGuestCapabilityCompatibilityAndSessionValidation(t *testing.T) {
	server := miniredis.RunT(t)
	cache := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	service := &svc.ServiceContext{Redis: cache}
	logic := NewQueryPurchaseOrderLogic(context.Background(), service)
	for _, info := range []*order.Order{
		nil,
		{Status: order.StatusPaid},
		{UserId: 7, Status: order.StatusPaid},
		{UserId: 7, GuestCheckoutTokenHash: constant.CheckoutTokenHash("capability"), Status: order.StatusPending},
	} {
		if _, err := IssuePurchaseSession(context.Background(), service, info); err == nil {
			t.Fatalf("invalid guest session input %+v was accepted", info)
		}
	}
	if err := logic.authorizeGuestCapability(&order.Order{OrderNo: "legacy"}, ""); err == nil {
		t.Fatal("missing guest capability was accepted")
	}
	if err := logic.authorizeGuestCapability(&order.Order{GuestCheckoutTokenHash: constant.CheckoutTokenHash("owner")}, "other"); err == nil {
		t.Fatal("wrong persisted capability was accepted")
	}
	legacy := constant.TemporaryOrderInfo{OrderNo: "legacy", CheckoutToken: "legacy-token"}
	encoded, err := legacy.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Set(context.Background(), fmt.Sprintf(constant.TempOrderCacheKey, "legacy"), string(encoded), 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := logic.authorizeGuestCapability(&order.Order{OrderNo: "legacy"}, "legacy-token"); err != nil {
		t.Fatal(err)
	}
	if err := logic.authorizeGuestCapability(&order.Order{OrderNo: "legacy"}, "wrong"); err == nil {
		t.Fatal("wrong legacy capability was accepted")
	}
}

func TestGuestSessionRejectsWrongCapabilityBeforeStorage(t *testing.T) {
	logic := NewQueryPurchaseOrderLogic(context.Background(), nil)
	info := &order.Order{UserId: 42, GuestCheckoutTokenHash: constant.CheckoutTokenHash("owner-capability")}
	if _, err := logic.handleTemporaryOrder(info, "other-capability"); err == nil {
		t.Fatal("guest session accepted a different checkout capability")
	}
}
