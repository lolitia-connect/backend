package order

import (
	"context"
	"errors"
	"testing"

	"github.com/perfect-panel/server/internal/model/coupon"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/model/wallet"
	"github.com/perfect-panel/server/internal/orderflow"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
)

func TestCreateOrderRejectsInvalidFulfillmentInput(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*types.CreateOrderRequest)
	}{
		{"unknown type", func(request *types.CreateOrderRequest) { request.Type = 9 }},
		{"paid initial state", func(request *types.CreateOrderRequest) { request.Status = order.StatusPaid }},
		{"negative quantity", func(request *types.CreateOrderRequest) { request.Quantity = -1 }},
		{"large quantity", func(request *types.CreateOrderRequest) { request.Quantity = 1001 }},
		{"gateway trade", func(request *types.CreateOrderRequest) { request.TradeNo = "cs_unrelated" }},
		{"missing plan", func(request *types.CreateOrderRequest) { request.SubscribeId = 0 }},
		{"missing renewal target", func(request *types.CreateOrderRequest) { request.Type = order.TypeRenewal }},
		{"recharge coupon", func(request *types.CreateOrderRequest) {
			request.Type, request.SubscribeId, request.Coupon = order.TypeRecharge, 0, "FREE"
		}},
		{"negative money", func(request *types.CreateOrderRequest) { request.Amount = -1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := types.CreateOrderRequest{UserId: 1, Type: order.TypeSubscribe, Quantity: 1, SubscribeId: 7, PaymentId: 2, Amount: 100}
			test.change(&request)
			if err := validateCreateOrder(&request); err == nil {
				t.Fatal("invalid admin order accepted")
			}
		})
	}
	for _, request := range []types.CreateOrderRequest{
		{UserId: 1, Type: order.TypeSubscribe, SubscribeId: 7, PaymentId: 2},
		{UserId: 1, Type: order.TypeRenewal, UserSubscribeId: 8, PaymentId: 2},
		{UserId: 1, Type: order.TypeResetTraffic, UserSubscribeId: 8, PaymentId: 2},
		{UserId: 1, Type: order.TypeRecharge, PaymentId: -1},
	} {
		if err := validateCreateOrder(&request); err != nil {
			t.Fatalf("type %d: %v", request.Type, err)
		}
	}
}

type adminOrders struct {
	order.Model
	created       *order.Order
	guestCount    int64
	guestIdentity string
}

func (model *adminOrders) Insert(_ context.Context, current *order.Order) error {
	saved := *current
	model.created = &saved
	return nil
}

func (model *adminOrders) CountUserCouponUsage(context.Context, int64, string) (int64, error) {
	return 0, nil
}
func (model *adminOrders) CountGuestCouponUsage(_ context.Context, authType, identifier, code string) (int64, error) {
	model.guestIdentity = authType + ":" + identifier + ":" + code
	return model.guestCount, nil
}

type adminPayments struct{ payment.Model }

func (*adminPayments) FindOne(context.Context, int64) (*payment.Payment, error) {
	return &payment.Payment{Id: 2, Platform: "EPay", Token: "callback-secret"}, nil
}

type adminUsers struct {
	user.Model
	target user.Subscribe
}

func (*adminUsers) FindOne(context.Context, int64) (*user.User, error) { return &user.User{Id: 1}, nil }
func (model *adminUsers) FindOneSubscribeForUpdate(context.Context, int64) (*user.Subscribe, error) {
	current := model.target
	return &current, nil
}

type adminWallets struct{ wallet.Model }

func (*adminWallets) FindOneForUpdate(context.Context, int64) (*wallet.Wallet, error) {
	return &wallet.Wallet{UserId: 1}, nil
}

type adminPlans struct {
	subscribe.Model
	inventory int
}

func (*adminPlans) FindOne(context.Context, int64) (*subscribe.Subscribe, error) {
	return &subscribe.Subscribe{Id: 7}, nil
}
func (model *adminPlans) ReserveInventory(context.Context, int64) error {
	if model.inventory <= 0 {
		return errors.New("out of stock")
	}
	model.inventory--
	return nil
}

type adminCoupons struct {
	coupon.Model
	used      int
	userLimit int64
}

func (model *adminCoupons) FindOneByCode(context.Context, string) (*coupon.Coupon, error) {
	enabled := true
	return &coupon.Coupon{Code: "SAVE", Enable: &enabled, Subscribe: "7", UserLimit: model.userLimit}, nil
}
func (model *adminCoupons) ReserveUsage(context.Context, string) error { model.used++; return nil }

type adminLogs struct {
	log.Model
	fail bool
}

func (model *adminLogs) Insert(context.Context, *log.SystemLog) error {
	if model.fail {
		return errors.New("audit failed")
	}
	return nil
}

type adminOrderStore struct {
	repository.Store
	orders   adminOrders
	payments adminPayments
	users    adminUsers
	wallets  adminWallets
	plans    adminPlans
	coupons  adminCoupons
	logs     adminLogs
}

func (store *adminOrderStore) Order() order.Model         { return &store.orders }
func (store *adminOrderStore) Payment() payment.Model     { return &store.payments }
func (store *adminOrderStore) User() user.Model           { return &store.users }
func (store *adminOrderStore) Wallet() wallet.Model       { return &store.wallets }
func (store *adminOrderStore) Subscribe() subscribe.Model { return &store.plans }
func (store *adminOrderStore) Coupon() coupon.Model       { return &store.coupons }
func (store *adminOrderStore) Log() log.Model             { return &store.logs }
func (store *adminOrderStore) InTx(_ context.Context, action func(repository.Store) error) error {
	created, inventory, used := store.orders.created, store.plans.inventory, store.coupons.used
	if err := action(store); err != nil {
		store.orders.created, store.plans.inventory, store.coupons.used = created, inventory, used
		return err
	}
	return nil
}

func TestAdminOrderReservesAndRollsBackResources(t *testing.T) {
	store := &adminOrderStore{plans: adminPlans{inventory: 1}}
	logic := NewCreateOrderLogic(context.Background(), &svc.ServiceContext{Store: store})
	request := &types.CreateOrderRequest{UserId: 1, Type: order.TypeSubscribe, SubscribeId: 7, PaymentId: 2, Amount: 100, Coupon: "SAVE"}
	store.logs.fail = true
	if err := logic.CreateOrder(request); err == nil {
		t.Fatal("audit failure accepted")
	}
	if store.orders.created != nil || store.plans.inventory != 1 || store.coupons.used != 0 {
		t.Fatal("failed creation retained reservations")
	}
	store.logs.fail = false
	store.plans.inventory = 0
	if err := logic.CreateOrder(request); err == nil {
		t.Fatal("out of stock accepted")
	}
	if store.orders.created != nil || store.coupons.used != 0 {
		t.Fatal("inventory failure retained coupon")
	}
	store.plans.inventory = 1
	if err := logic.CreateOrder(request); err != nil {
		t.Fatal(err)
	}
	current := store.orders.created
	if current == nil || current.Quantity != 1 || current.Status != order.StatusPending || current.Method != "EPay" || !current.CouponReserved || store.plans.inventory != 0 || store.coupons.used != 1 {
		t.Fatalf("invalid created order: %+v", current)
	}
}

func TestAdminRenewalBindsStableSubscription(t *testing.T) {
	store := &adminOrderStore{users: adminUsers{target: user.Subscribe{Id: 8, UserId: 1, SubscribeId: 7, OrderId: 3, Token: "rotated", Status: 1}}}
	logic := NewCreateOrderLogic(context.Background(), &svc.ServiceContext{Store: store})
	request := &types.CreateOrderRequest{UserId: 1, Type: order.TypeRenewal, UserSubscribeId: 8, PaymentId: 2}
	if err := logic.CreateOrder(request); err != nil {
		t.Fatal(err)
	}
	current := store.orders.created
	if current.UserSubscribeId != 8 || current.SubscribeId != 7 || current.ParentId != 3 || current.SubscribeToken != "rotated" {
		t.Fatalf("subscription not bound: %+v", current)
	}
	store.orders.created = nil
	store.users.target.UserId = 9
	if err := logic.CreateOrder(request); err == nil || store.orders.created != nil {
		t.Fatal("another owner's subscription accepted")
	}
}

func TestAdminProviderOrderDoesNotReserveResources(t *testing.T) {
	for _, orderType := range []uint8{order.TypeRenewal, order.TypeResetTraffic} {
		store := &adminOrderStore{users: adminUsers{target: user.Subscribe{Id: 8, UserId: 1, SubscribeId: 7, Status: 1, EntitlementSource: "apple"}}}
		logic := NewCreateOrderLogic(context.Background(), &svc.ServiceContext{Store: store})
		request := &types.CreateOrderRequest{UserId: 1, Type: orderType, UserSubscribeId: 8, PaymentId: 2, Coupon: "SAVE"}
		if err := logic.CreateOrder(request); err == nil {
			t.Fatalf("provider order type %d was accepted", orderType)
		}
		if store.orders.created != nil || store.coupons.used != 0 || store.plans.inventory != 0 {
			t.Fatalf("provider order type %d reserved resources", orderType)
		}
	}
}

func TestGuestCouponLimitUsesDurableIdentityAndRollsBack(t *testing.T) {
	store := &adminOrderStore{coupons: adminCoupons{userLimit: 1}}
	current := &order.Order{Type: order.TypeSubscribe, SubscribeId: 7, Coupon: "SAVE", GuestAuthType: "email", GuestIdentifier: "guest@example.com"}
	store.orders.guestCount = 1
	err := store.InTx(context.Background(), func(transaction repository.Store) error {
		return orderflow.ReserveCoupon(context.Background(), transaction, current)
	})
	if err == nil || current.CouponReserved || store.coupons.used != 0 {
		t.Fatal("guest limit bypassed or reservation retained")
	}
	if store.orders.guestIdentity != "email:guest@example.com:SAVE" {
		t.Fatal("guest usage counted by another identity")
	}
	store.orders.guestCount = 0
	if err := store.InTx(context.Background(), func(transaction repository.Store) error {
		return orderflow.ReserveCoupon(context.Background(), transaction, current)
	}); err != nil {
		t.Fatal(err)
	}
	if !current.CouponReserved || store.coupons.used != 1 {
		t.Fatal("eligible guest did not reserve coupon")
	}
}
