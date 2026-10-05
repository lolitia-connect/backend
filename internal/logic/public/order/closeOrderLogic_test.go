package order

import (
	"testing"

	"github.com/perfect-panel/server/internal/model/order"
)

// Closing an order restores plan stock. Renewals and traffic resets reference a
// plan but never take stock from it, so they must not be treated as reserving
// ones - doing so inflated plan inventory every time such an order was closed.
func TestReservesPlanInventoryOnlyForNewSubscriptionPurchases(t *testing.T) {
	tests := []struct {
		name    string
		order   *order.Order
		reserve bool
	}{
		{
			name:    "new subscription purchase reserves stock",
			order:   &order.Order{Type: order.TypeSubscribe, SubscribeId: 7},
			reserve: true,
		},
		{
			name:    "renewal does not reserve stock",
			order:   &order.Order{Type: order.TypeRenewal, SubscribeId: 7},
			reserve: false,
		},
		{
			name:    "traffic reset does not reserve stock",
			order:   &order.Order{Type: order.TypeResetTraffic, SubscribeId: 7},
			reserve: false,
		},
		{
			name:    "wallet recharge does not reserve stock",
			order:   &order.Order{Type: order.TypeRecharge, SubscribeId: 7},
			reserve: false,
		},
		{
			name:    "purchase without a plan reserves nothing",
			order:   &order.Order{Type: order.TypeSubscribe},
			reserve: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reservesPlanInventory(tt.order); got != tt.reserve {
				t.Fatalf("reservesPlanInventory(%+v) = %v, want %v", tt.order, got, tt.reserve)
			}
		})
	}
}
