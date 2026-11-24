package portal

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
)

type billDescSubscriptions struct {
	subscribe.Model
}

func (*billDescSubscriptions) FindOne(_ context.Context, id int64) (*subscribe.Subscribe, error) {
	if id == 7 {
		return &subscribe.Subscribe{Name: "月度套餐"}, nil
	}
	return nil, errors.New("subscription not found")
}

type billDescStore struct {
	repository.Store
}

func (*billDescStore) Subscribe() subscribe.Model { return &billDescSubscriptions{} }

func TestResolveBillDescItemName(t *testing.T) {
	logic := NewPurchaseCheckoutLogic(t.Context(), &svc.ServiceContext{Store: &billDescStore{}})
	for _, tc := range []struct {
		name        string
		kind        uint8
		subscribeID int64
		itemName    string
	}{
		{"recharge without subscription", order.TypeRecharge, 0, "充值"},
		{"recharge with subscription", order.TypeRecharge, 7, "充值"},
		{"subscription purchase", order.TypeSubscribe, 7, "月度套餐"},
		{"subscription renewal", order.TypeRenewal, 7, "月度套餐"},
		{"traffic reset", order.TypeResetTraffic, 7, "月度套餐"},
		{"missing subscription", order.TypeSubscribe, 8, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &order.Order{OrderNo: "invoice-1", Type: tc.kind, SubscribeId: tc.subscribeID, Amount: 1234}
			got := logic.resolveBillDesc("{item_name}/{order_no}/{amount}/{trade_no}/{item_name}", info, "trade-1")
			want := tc.itemName + "/invoice-1/12.34/trade-1/" + tc.itemName
			if got != want {
				t.Fatalf("billing description = %q, want %q", got, want)
			}
		})
	}
}

func TestResolveBillDescEmptyTemplate(t *testing.T) {
	logic := NewPurchaseCheckoutLogic(t.Context(), nil)
	if got := logic.resolveBillDesc("", nil, ""); got != "" {
		t.Fatalf("empty billing description = %q", got)
	}
}
