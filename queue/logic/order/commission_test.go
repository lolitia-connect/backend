package orderLogic

import (
	"context"
	"errors"
	"fmt"
	"github.com/perfect-panel/server/internal/model/coupon"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/model/wallet"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/redis/go-redis/v9"
)

func TestCalculateCommission(t *testing.T) {
	l := &ActivateOrderLogic{}
	cases := []struct {
		name       string
		price      int64
		percentage uint8
		want       int64
	}{
		{"zero percentage", 10000, 0, 0},
		{"ten percent", 10000, 10, 1000},
		{"fractional truncates", 9999, 10, 999},
		{"hundred percent", 500, 100, 500},
		{"zero price", 0, 30, 0},
		{"exact percentage", 100, 29, 29},
		{"clamped percentage", 1000, 150, 1000},
		{"maximum percentage", 1000, 255, 1000},
		{"large price", 9223372036854775807, 100, 9223372036854775807},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := l.calculateCommission(c.price, c.percentage); got != c.want {
				t.Fatalf("calculateCommission(%d, %d) = %d, want %d", c.price, c.percentage, got, c.want)
			}
		})
	}
}

// The commission handler skips processing when the computed amount is not
// positive, so these values must never produce a log entry.
func TestCalculateCommissionNonPositiveTriggersSkip(t *testing.T) {
	l := &ActivateOrderLogic{}
	if amount := l.calculateCommission(0, 50); amount > 0 {
		t.Fatalf("zero-price order must not accrue commission, got %d", amount)
	}
	if amount := l.calculateCommission(-100, 50); amount > 0 {
		t.Fatalf("negative-price order must not accrue commission, got %d", amount)
	}
}

type subscriptionLookup struct {
	byID       *user.Subscribe
	byToken    *user.Subscribe
	tokenCalls int
}

func (lookup *subscriptionLookup) FindOneSubscribe(context.Context, int64) (*user.Subscribe, error) {
	if lookup.byID == nil {
		return nil, errors.New("subscription missing")
	}
	return lookup.byID, nil
}

func (lookup *subscriptionLookup) FindOneSubscribeByToken(context.Context, string) (*user.Subscribe, error) {
	lookup.tokenCalls++
	return lookup.byToken, nil
}

func TestOrderSubscriptionSurvivesTokenRotation(t *testing.T) {
	lookup := &subscriptionLookup{byID: &user.Subscribe{Id: 3, UserId: 7, SubscribeId: 9, Token: "rotated"}}
	info := &order.Order{UserId: 7, SubscribeId: 9, UserSubscribeId: 3, SubscribeToken: "old-token"}
	got, err := findOrderSubscription(context.Background(), lookup, info)
	if err != nil || got != lookup.byID || lookup.tokenCalls != 0 {
		t.Fatalf("lookup after rotation = %+v, %v, token calls %d", got, err, lookup.tokenCalls)
	}
	lookup.byID = nil
	lookup.byToken = &user.Subscribe{UserId: 7, SubscribeId: 9}
	if _, err := findOrderSubscription(context.Background(), lookup, info); err == nil || lookup.tokenCalls != 0 {
		t.Fatal("missing stable ID fell back to a token")
	}
	info.UserSubscribeId = 0
	if _, err := findOrderSubscription(context.Background(), lookup, info); err != nil || lookup.tokenCalls != 1 {
		t.Fatalf("legacy token lookup failed: %v", err)
	}
	info.UserId = 8
	if _, err := findOrderSubscription(context.Background(), lookup, info); err == nil {
		t.Fatal("another owner's subscription was accepted")
	}
}

type activationOrders struct {
	order.Model
	rows      map[string]order.Order
	finishErr error
}

func (model *activationOrders) FindOneByOrderNoForUpdate(_ context.Context, number string) (*order.Order, error) {
	current := model.rows[number]
	return &current, nil
}

func (model *activationOrders) Update(_ context.Context, current *order.Order) error {
	model.rows[current.OrderNo] = *current
	return nil
}

func (model *activationOrders) FinishOrder(_ context.Context, number string) (bool, error) {
	if model.finishErr != nil {
		return false, model.finishErr
	}
	current := model.rows[number]
	if current.Status != order.StatusPaid {
		return false, nil
	}
	current.Status = order.StatusFinished
	model.rows[number] = current
	return true, nil
}

func (model *activationOrders) ClosePaidOrder(_ context.Context, number string) (bool, error) {
	current := model.rows[number]
	if current.Status != order.StatusPaid {
		return false, nil
	}
	current.Status = order.StatusClosed
	model.rows[number] = current
	return true, nil
}

func (model *activationOrders) SetCommission(_ context.Context, id, amount, refererID int64) error {
	for number, current := range model.rows {
		if current.Id == id {
			current.Commission, current.CommissionRefererId = amount, refererID
			model.rows[number] = current
		}
	}
	return nil
}

func (model *activationOrders) HasCommissionedOrder(_ context.Context, userID int64, excluding string) (bool, error) {
	for number, current := range model.rows {
		if number != excluding && current.UserId == userID && current.Commission > 0 {
			return true, nil
		}
	}
	return false, nil
}

type activationWallets struct {
	wallet.Model
	rows map[int64]wallet.Wallet
}

func (model *activationWallets) FindOneForUpdate(_ context.Context, userID int64) (*wallet.Wallet, error) {
	current := model.rows[userID]
	current.UserId = userID
	return &current, nil
}

func (model *activationWallets) UpdateBalanceFields(_ context.Context, current *wallet.Wallet) error {
	model.rows[current.UserId] = *current
	return nil
}

func (model *activationWallets) UpdateCommission(_ context.Context, current *wallet.Wallet) error {
	model.rows[current.UserId] = *current
	return nil
}

type activationUsers struct {
	user.Model
	rows         map[int64]user.User
	subscription user.Subscribe
	identity     *user.AuthMethods
}

func (model *activationUsers) FindUserAuthMethodByOpenID(context.Context, string, string) (*user.AuthMethods, error) {
	return model.identity, nil
}

func (model *activationUsers) FindOneSubscribe(context.Context, int64) (*user.Subscribe, error) {
	current := model.subscription
	return &current, nil
}

func (model *activationUsers) FindOneSubscribeForUpdate(ctx context.Context, id int64) (*user.Subscribe, error) {
	return model.FindOneSubscribe(ctx, id)
}

func (model *activationUsers) FindOne(_ context.Context, id int64) (*user.User, error) {
	current := model.rows[id]
	return &current, nil
}

type activationLogs struct {
	log.Model
	rows      []log.SystemLog
	insertErr error
}

func (model *activationLogs) Insert(_ context.Context, row *log.SystemLog) error {
	if model.insertErr != nil {
		return model.insertErr
	}
	model.rows = append(model.rows, *row)
	return nil
}

type activationTestStore struct {
	repository.Store
	mutex   sync.Mutex
	orders  activationOrders
	wallets activationWallets
	users   activationUsers
	logs    activationLogs
	coupons activationCoupons
	plans   activationPlans
}

type activationPlans struct {
	subscribe.Model
	restored int
}

func (model *activationPlans) RestoreInventory(context.Context, int64) error {
	model.restored++
	return nil
}

type activationCoupons struct {
	coupon.Model
	counted  int
	released int
}

func (model *activationCoupons) UpdateCount(context.Context, string) error {
	model.counted++
	return nil
}
func (model *activationCoupons) ReleaseUsage(context.Context, string) error {
	model.released++
	return nil
}

func (store *activationTestStore) Ent() *ent.Client           { return nil }
func (store *activationTestStore) Order() order.Model         { return &store.orders }
func (store *activationTestStore) Wallet() wallet.Model       { return &store.wallets }
func (store *activationTestStore) User() user.Model           { return &store.users }
func (store *activationTestStore) Log() log.Model             { return &store.logs }
func (store *activationTestStore) Coupon() coupon.Model       { return &store.coupons }
func (store *activationTestStore) Subscribe() subscribe.Model { return &store.plans }

func (store *activationTestStore) InTx(_ context.Context, action func(repository.Store) error) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	orders := make(map[string]order.Order, len(store.orders.rows))
	for number, current := range store.orders.rows {
		orders[number] = current
	}
	wallets := make(map[int64]wallet.Wallet, len(store.wallets.rows))
	for userID, current := range store.wallets.rows {
		wallets[userID] = current
	}
	logCount := len(store.logs.rows)
	counted, released := store.coupons.counted, store.coupons.released
	restored := store.plans.restored
	if err := action(store); err != nil {
		store.orders.rows, store.wallets.rows = orders, wallets
		store.logs.rows = store.logs.rows[:logCount]
		store.coupons.counted, store.coupons.released = counted, released
		store.plans.restored = restored
		return err
	}
	return nil
}

func TestRechargeActivationRollsBackAndReplaysOnce(t *testing.T) {
	store := &activationTestStore{
		orders:  activationOrders{rows: map[string]order.Order{"recharge": {Id: 1, OrderNo: "recharge", UserId: 7, Type: OrderTypeRecharge, Status: order.StatusPaid, Price: 100}}},
		wallets: activationWallets{rows: map[int64]wallet.Wallet{7: {UserId: 7, Balance: 10}}},
		users:   activationUsers{rows: map[int64]user.User{7: {Id: 7}}},
	}
	logic := NewActivateOrderLogic(&svc.ServiceContext{Store: store})
	task := asynq.NewTask("test", []byte(`{"order_no":"recharge"}`))
	store.logs.insertErr = errors.New("ledger unavailable")
	if err := logic.ProcessTask(context.Background(), task); err == nil {
		t.Fatal("ledger failure was acknowledged")
	}
	if store.wallets.rows[7].Balance != 10 || store.orders.rows["recharge"].Status != order.StatusPaid {
		t.Fatal("failed activation committed a partial credit")
	}
	store.logs.insertErr = nil
	for attempt := 0; attempt < 2; attempt++ {
		if err := logic.ProcessTask(context.Background(), task); err != nil {
			t.Fatal(err)
		}
	}
	if store.wallets.rows[7].Balance != 110 || len(store.logs.rows) != 1 || store.orders.rows["recharge"].Status != order.StatusFinished {
		t.Fatal("redelivery credited recharge more than once")
	}
}

func TestAppleOrderCannotEnterLocalFulfillment(t *testing.T) {
	store := &activationTestStore{
		orders: activationOrders{rows: map[string]order.Order{
			"apple": {Id: 1, OrderNo: "apple", Method: "AppleIAP", UserId: 7, Type: OrderTypeSubscribe, Status: order.StatusPaid, Amount: 100, GiftAmount: 50, Coupon: "SAVE", CouponReserved: true},
		}},
		wallets: activationWallets{rows: map[int64]wallet.Wallet{7: {UserId: 7, Balance: 10, GiftAmount: 20}}},
	}
	logic := NewActivateOrderLogic(&svc.ServiceContext{Store: store})
	task := asynq.NewTask("test", []byte(`{"order_no":"apple"}`))
	if err := logic.ProcessTask(context.Background(), task); !errors.Is(err, user.ErrProviderManaged) {
		t.Fatalf("Apple fulfillment error = %v, want ErrProviderManaged", err)
	}
	if store.wallets.rows[7].Balance != 10 || store.wallets.rows[7].GiftAmount != 20 || store.orders.rows["apple"].Status != order.StatusPaid || len(store.logs.rows) != 0 || store.plans.restored != 0 || store.coupons.released != 0 {
		t.Fatal("provider order changed local wallet, order, inventory, coupon, or audit log")
	}
}

func TestCommissionSettlementRecordsRefererAndFirstPurchaseOnce(t *testing.T) {
	onlyFirst := true
	store := &activationTestStore{
		orders: activationOrders{rows: map[string]order.Order{
			"first":  {Id: 1, OrderNo: "first", UserId: 7, Type: OrderTypeSubscribe, Status: order.StatusPaid, Amount: 1000, IsNew: true},
			"second": {Id: 2, OrderNo: "second", UserId: 7, Type: OrderTypeSubscribe, Status: order.StatusPaid, Amount: 1000, IsNew: true},
		}},
		wallets: activationWallets{rows: map[int64]wallet.Wallet{11: {UserId: 11, Commission: 10}}},
		users:   activationUsers{rows: map[int64]user.User{7: {Id: 7, RefererId: 11}, 11: {Id: 11, ReferralPercentage: 20, OnlyFirstPurchase: &onlyFirst}}},
	}
	logic := NewActivateOrderLogic(&svc.ServiceContext{Store: store})
	first := store.orders.rows["first"]
	store.orders.finishErr = errors.New("event insert failed")
	if err := logic.finalizeCouponAndOrder(context.Background(), &first); err == nil {
		t.Fatal("failed order transition was acknowledged")
	}
	if store.wallets.rows[11].Commission != 10 || store.orders.rows["first"].Commission != 0 || len(store.logs.rows) != 0 {
		t.Fatal("commission survived a failed finalization")
	}
	store.orders.finishErr = nil
	for _, number := range []string{"first", "first", "second"} {
		current := store.orders.rows[number]
		if err := logic.finalizeCouponAndOrder(context.Background(), &current); err != nil {
			t.Fatal(err)
		}
	}
	if store.wallets.rows[11].Commission != 210 || len(store.logs.rows) != 1 {
		t.Fatal("first purchase commission paid more than once")
	}
	if credited := store.orders.rows["first"]; credited.Commission != 200 || credited.CommissionRefererId != 11 {
		t.Fatal("credited referrer and amount were not persisted")
	}
	if store.orders.rows["second"].Commission != 0 {
		t.Fatal("second simultaneous new order received first-purchase commission")
	}
}

func TestPaidRenewalOfCanceledSubscriptionRefundsOnce(t *testing.T) {
	store := &activationTestStore{
		orders:  activationOrders{rows: map[string]order.Order{"renewal": {Id: 1, OrderNo: "renewal", UserId: 7, SubscribeId: 9, UserSubscribeId: 3, Type: OrderTypeRenewal, Status: order.StatusPaid, Amount: 100, GiftAmount: 50, Coupon: "SAVE", CouponReserved: true}}},
		wallets: activationWallets{rows: map[int64]wallet.Wallet{7: {UserId: 7, Balance: 10, GiftAmount: 20}}},
		users:   activationUsers{rows: map[int64]user.User{7: {Id: 7}}, subscription: user.Subscribe{Id: 3, UserId: 7, SubscribeId: 9, Status: 4}},
	}
	logic := NewActivateOrderLogic(&svc.ServiceContext{Store: store})
	task := asynq.NewTask("test", []byte(`{"order_no":"renewal"}`))
	store.logs.insertErr = errors.New("refund audit unavailable")
	if err := logic.ProcessTask(context.Background(), task); err == nil {
		t.Fatal("failed refund was acknowledged")
	}
	if store.wallets.rows[7].Balance != 10 || store.orders.rows["renewal"].Status != order.StatusPaid {
		t.Fatal("refund partially committed")
	}
	store.logs.insertErr = nil
	for attempt := 0; attempt < 2; attempt++ {
		if err := logic.ProcessTask(context.Background(), task); err != nil {
			t.Fatal(err)
		}
	}
	if store.wallets.rows[7].Balance != 110 || store.wallets.rows[7].GiftAmount != 70 || len(store.logs.rows) != 2 || store.orders.rows["renewal"].Status != order.StatusClosed {
		t.Fatal("unfulfillable renewal was not refunded exactly once")
	}
	if store.users.subscription.Status != 4 {
		t.Fatal("refunded subscription was revived")
	}
	if store.coupons.released != 1 || store.coupons.counted != 0 {
		t.Fatal("refunded coupon was not released exactly once")
	}
}

func TestActivationConsumesReservedAndLegacyCouponsOnce(t *testing.T) {
	for _, reserved := range []bool{false, true} {
		store := &activationTestStore{orders: activationOrders{rows: map[string]order.Order{"paid": {OrderNo: "paid", Type: order.TypeSubscribe, Status: order.StatusPaid, Coupon: "SAVE", CouponReserved: reserved}}}}
		logic := NewActivateOrderLogic(&svc.ServiceContext{Store: store})
		for attempt := 0; attempt < 2; attempt++ {
			current := store.orders.rows["paid"]
			if err := logic.finalizeCouponAndOrder(context.Background(), &current); err != nil {
				t.Fatal(err)
			}
		}
		want := 1
		if reserved {
			want = 0
		}
		if store.coupons.counted != want || store.coupons.released != 0 {
			t.Fatalf("reserved=%t, counted=%d, released=%d", reserved, store.coupons.counted, store.coupons.released)
		}
	}
}

func TestGuestIdentityCollisionRefundsExistingAccountOnce(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	temporary := constant.TemporaryOrderInfo{OrderNo: "guest", AuthType: "email", Identifier: "owner@example.com", Password: "guest-password"}
	encoded, err := temporary.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Set(context.Background(), fmt.Sprintf(constant.TempOrderCacheKey, "guest"), string(encoded), time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	store := &activationTestStore{
		orders:  activationOrders{rows: map[string]order.Order{"guest": {Id: 1, OrderNo: "guest", Type: order.TypeSubscribe, Status: order.StatusPaid, SubscribeId: 9, Amount: 100, GiftAmount: 50, Coupon: "SAVE", CouponReserved: true}}},
		wallets: activationWallets{rows: map[int64]wallet.Wallet{7: {UserId: 7, Balance: 10, GiftAmount: 20}}},
		users:   activationUsers{rows: map[int64]user.User{7: {Id: 7}}, identity: &user.AuthMethods{UserId: 7, AuthType: "email", AuthIdentifier: "owner@example.com"}},
	}
	logic := NewActivateOrderLogic(&svc.ServiceContext{Store: store, Redis: client})
	task := asynq.NewTask("test", []byte(`{"order_no":"guest"}`))
	store.logs.insertErr = errors.New("refund audit unavailable")
	if err := logic.ProcessTask(context.Background(), task); err == nil {
		t.Fatal("failed guest refund was acknowledged")
	}
	if store.orders.rows["guest"].UserId != 0 || store.wallets.rows[7].Balance != 10 || store.plans.restored != 0 {
		t.Fatal("guest refund partially committed")
	}
	store.logs.insertErr = nil
	for attempt := 0; attempt < 2; attempt++ {
		if err := logic.ProcessTask(context.Background(), task); err != nil {
			t.Fatal(err)
		}
	}
	current := store.orders.rows["guest"]
	if current.UserId != 7 || current.Status != order.StatusClosed || store.wallets.rows[7].Balance != 110 || store.wallets.rows[7].GiftAmount != 70 || store.plans.restored != 1 || store.coupons.released != 1 || len(store.logs.rows) != 2 {
		t.Fatal("guest identity collision did not refund exactly once")
	}
}
