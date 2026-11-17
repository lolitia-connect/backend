package user

import (
	"context"
	"errors"
	"reflect"
	"testing"

	ordermodel "github.com/perfect-panel/server/internal/model/order"
	usermodel "github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
)

type refundUserModel struct {
	usermodel.Model
	subscription *usermodel.Subscribe
	details      *usermodel.SubscribeDetails
}

func (model *refundUserModel) FindOneSubscribe(context.Context, int64) (*usermodel.Subscribe, error) {
	return model.subscription, nil
}
func (model *refundUserModel) FindOneUserSubscribe(context.Context, int64) (*usermodel.SubscribeDetails, error) {
	return model.details, nil
}

type refundStore struct {
	repository.Store
	users        *refundUserModel
	transactions int
}

func (store *refundStore) User() usermodel.Model { return store.users }
func (store *refundStore) InTx(ctx context.Context, action func(repository.Store) error) error {
	store.transactions++
	return action(store)
}

func TestRefundRejectsOtherOwnersBeforeTransaction(t *testing.T) {
	store := &refundStore{users: &refundUserModel{subscription: &usermodel.Subscribe{Id: 3, UserId: 8, Status: 1}, details: &usermodel.SubscribeDetails{Id: 3, UserId: 8, Status: 1}}}
	service := &svc.ServiceContext{Store: store}
	ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &usermodel.User{Id: 7})
	if err := NewUnsubscribeLogic(ctx, service).Unsubscribe(&types.UnsubscribeRequest{Id: 3}); err == nil {
		t.Fatal("another owner's refund was accepted")
	}
	if _, err := CalculateRemainingAmount(ctx, service, 3); err == nil {
		t.Fatal("another owner's refund quote was disclosed")
	}
	if store.transactions != 0 {
		t.Fatal("unauthorized refund opened a transaction")
	}
}

func TestProviderSubscriptionRejectsRefundAndQuote(t *testing.T) {
	store := &refundStore{users: &refundUserModel{
		subscription: &usermodel.Subscribe{Id: 3, UserId: 7, Status: 1, EntitlementSource: "apple"},
		details:      &usermodel.SubscribeDetails{Id: 3, UserId: 7, Status: 1, EntitlementSource: "apple"},
	}}
	service := &svc.ServiceContext{Store: store}
	ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &usermodel.User{Id: 7})
	if err := NewUnsubscribeLogic(ctx, service).Unsubscribe(&types.UnsubscribeRequest{Id: 3}); !errors.Is(err, usermodel.ErrProviderManaged) {
		t.Fatalf("provider refund error = %v", err)
	}
	if _, err := CalculateRemainingAmount(ctx, service, 3); !errors.Is(err, usermodel.ErrProviderManaged) {
		t.Fatalf("provider refund quote error = %v", err)
	}
	if store.transactions != 0 {
		t.Fatal("provider refund opened a local settlement transaction")
	}
}

func TestRefundRejectsCanceledAndDeletedPlan(t *testing.T) {
	for _, status := range []uint8{1, 4} {
		t.Run(string(rune('0'+status)), func(t *testing.T) {
			store := &refundStore{users: &refundUserModel{details: &usermodel.SubscribeDetails{Id: 3, UserId: 7, OrderId: 9, Status: status}}}
			ctx := context.WithValue(context.Background(), constant.CtxKeyUser, &usermodel.User{Id: 7})
			if _, err := CalculateRemainingAmount(ctx, &svc.ServiceContext{Store: store}, 3); err == nil {
				t.Fatal("canceled subscription or missing plan accepted")
			}
		})
	}
}

func TestRefundUsesPaidRenewalsAndCreditedReferers(t *testing.T) {
	details := &ordermodel.Details{Amount: 900, GiftAmount: 100, Commission: 90, CommissionRefererId: 11, SubOrders: []*ordermodel.Order{
		{Type: 2, Status: ordermodel.StatusFinished, Amount: 800, GiftAmount: 200, Commission: 80, CommissionRefererId: 12},
		{Type: 3, Status: ordermodel.StatusFinished, Amount: 1000, GiftAmount: 1000, Commission: 100, CommissionRefererId: 13},
		{Type: 2, Status: ordermodel.StatusPending, Amount: 1000, Commission: 100, CommissionRefererId: 14},
	}}
	if details.RefundBasis() != 2000 || details.RefundGiftBasis() != 300 {
		t.Fatal("refund includes unpaid renewals or traffic reset orders")
	}
	got, err := refundedCommissions(details, 1000, 99)
	if err != nil || !reflect.DeepEqual(got, map[int64]int64{11: 45, 12: 40}) {
		t.Fatalf("reversals = %v, %v", got, err)
	}
	details.CommissionRefererId = 0
	got, err = refundedCommissions(details, 4000, 99)
	if err != nil || !reflect.DeepEqual(got, map[int64]int64{99: 90, 12: 80}) {
		t.Fatalf("historical capped reversals = %v, %v", got, err)
	}
}
