package orderLogic

import (
	"context"
	stdsql "database/sql"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/ent/systemlog"
	"github.com/perfect-panel/server/internal/model/entitlement"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestRecordLocalSubscriptionPeriods(t *testing.T) {
	database, err := stdsql.Open("sqlite", "file:local-period-test?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })

	logic := &ActivateOrderLogic{svc: &svc.ServiceContext{Ent: client}}
	now := time.Now().UTC().Truncate(time.Millisecond)
	subscription := &user.Subscribe{
		Id:         17,
		StartTime:  now.Add(-time.Hour),
		ExpireTime: now.Add(30 * 24 * time.Hour),
		Traffic:    100,
	}
	purchase := &order.Order{Id: 10, OrderNo: "local-purchase", SubscribeId: 3, Type: OrderTypeSubscribe}
	if err := logic.recordLocalSubscriptionPeriod(context.Background(), purchase, subscription, false); err != nil {
		t.Fatalf("record purchase period: %v", err)
	}

	renewalStart := subscription.ExpireTime
	renewal := &order.Order{Id: 11, OrderNo: "local-renewal", SubscribeId: 4, Type: OrderTypeRenewal}
	subscription.ExpireTime = renewalStart.Add(31 * 24 * time.Hour)
	if err := logic.recordLocalSubscriptionPeriod(context.Background(), renewal, subscription, true, renewalStart); err != nil {
		t.Fatalf("record renewal period: %v", err)
	}

	first, err := client.SubscriptionPeriod.Get(context.Background(), entitlement.LocalPeriodID(purchase.OrderNo))
	if err != nil {
		t.Fatal(err)
	}
	if first.EntitlementID != entitlement.LocalEntitlementID(subscription.Id) || first.OrderID != purchase.Id || first.PlanID != purchase.SubscribeId || first.TrafficReset {
		t.Fatalf("unexpected purchase period: %+v", first)
	}
	if !first.StartAt.Equal(subscription.StartTime) || !first.EndAt.Equal(renewalStart) {
		t.Fatalf("purchase period bounds = %v..%v, want %v..%v", first.StartAt, first.EndAt, subscription.StartTime, renewalStart)
	}

	second, err := client.SubscriptionPeriod.Get(context.Background(), entitlement.LocalPeriodID(renewal.OrderNo))
	if err != nil {
		t.Fatal(err)
	}
	if second.OrderID != renewal.Id || second.PlanID != renewal.SubscribeId || !second.TrafficReset || !second.StartAt.Equal(renewalStart) || !second.EndAt.Equal(subscription.ExpireTime) {
		t.Fatalf("unexpected renewal period: %+v", second)
	}
}

func TestPaidReferralPurchaseCommitsGrantAndCommissionTogether(t *testing.T) {
	service, _ := stripeTestService(t)
	ctx, db := t.Context(), service.Ent
	referrer := db.User.Create().SetPassword("fixture").SetReferralPercentage(25).SaveX(ctx)
	wallet := db.UserWallet.Create().SetUserID(referrer.ID).SetCommission(70).SaveX(ctx)
	buyer := db.User.Create().SetPassword("fixture").SetRefererID(referrer.ID).SaveX(ctx)
	db.UserWallet.Create().SetUserID(buyer.ID).SaveX(ctx)
	plan := db.Subscribe.Create().SetName("Monthly").SetUnitTime("Day").SetTraffic(500).SaveX(ctx)
	coupon := db.Coupon.Create().SetCode("LEGACY").SaveX(ctx)
	invoice := db.Order.Create().SetOrderNo("paid-referral").SetUserID(buyer.ID).SetSubscribeID(plan.ID).
		SetType(order.TypeSubscribe).SetStatus(order.StatusPaid).SetQuantity(30).SetPrice(1000).
		SetAmount(850).SetFeeAmount(50).SetIsNew(true).SetCoupon(coupon.Code).SaveX(ctx)
	activation := asynq.NewTask("activate", []byte(`{"order_no":"paid-referral"}`))
	logic := NewActivateOrderLogic(service)

	var failedWrite string
	unavailable := errors.New("fulfillment storage unavailable")
	db.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if mutation.Type() == failedWrite {
				return nil, unavailable
			}
			return next.Mutate(ctx, mutation)
		})
	})
	for _, failedWrite = range []string{ent.TypeUserSubscribe, ent.TypeSubscriptionPeriod, ent.TypeUserWallet, ent.TypeOrder, ent.TypeSystemLog, ent.TypeCoupon, ent.TypeOrderEvent} {
		require.ErrorIs(t, logic.ProcessTask(ctx, activation), unavailable, failedWrite)
		current := db.Order.GetX(ctx, invoice.ID)
		require.Equal(t, invoice.Status, current.Status)
		require.Equal(t, invoice.StateVersion, current.StateVersion)
		require.Zero(t, current.Commission)
		require.EqualValues(t, 70, db.UserWallet.GetX(ctx, wallet.ID).Commission)
		require.Zero(t, db.Coupon.GetX(ctx, coupon.ID).UsedCount)
		require.Zero(t, db.UserSubscribe.Query().CountX(ctx))
		require.Zero(t, db.SubscriptionPeriod.Query().CountX(ctx))
		require.Zero(t, db.SystemLog.Query().CountX(ctx))
		require.Zero(t, db.OrderEvent.Query().CountX(ctx))
	}
	failedWrite = ""
	for range 2 {
		require.NoError(t, logic.ProcessTask(ctx, activation))
	}
	current := db.Order.GetX(ctx, invoice.ID)
	require.EqualValues(t, order.StatusFinished, current.Status)
	require.Equal(t, invoice.StateVersion+1, current.StateVersion)
	require.Equal(t, referrer.ID, current.CommissionRefererID)
	require.EqualValues(t, 200, current.Commission)
	require.EqualValues(t, 270, db.UserWallet.GetX(ctx, wallet.ID).Commission)
	require.EqualValues(t, 1, db.Coupon.GetX(ctx, coupon.ID).UsedCount)
	subscription := db.UserSubscribe.Query().OnlyX(ctx)
	period := db.SubscriptionPeriod.Query().OnlyX(ctx)
	require.Equal(t, buyer.ID, subscription.UserID)
	require.Equal(t, subscription.ID, period.UserSubscribeID)
	require.Equal(t, invoice.OrderNo, period.TransactionKey)
	require.True(t, period.StartAt.AddDate(0, 0, 30).Equal(period.EndAt))
	event := db.OrderEvent.Query().OnlyX(ctx)
	require.Equal(t, order.EventOrderFulfilled, event.EventType)
	require.Equal(t, invoice.OrderNo, event.OrderNo)
	audit := db.SystemLog.Query().OnlyX(ctx)
	require.Equal(t, referrer.ID, audit.ObjectID)
	var receipt log.Commission
	require.NoError(t, receipt.Unmarshal([]byte(audit.Content)))
	require.Equal(t, invoice.OrderNo, receipt.OrderNo)
	require.EqualValues(t, 200, receipt.Amount)
	require.EqualValues(t, 270, receipt.Balance)
}

func TestPaidSubscriptionOrdersFulfillOnceOnRedelivery(t *testing.T) {
	service, _ := stripeTestService(t)
	ctx, db := t.Context(), service.Ent
	buyer := db.User.Create().SetPassword("fixture").SaveX(ctx)
	db.UserWallet.Create().SetUserID(buyer.ID).SaveX(ctx)
	plan := db.Subscribe.Create().SetName("Paid plan").SetUnitTime("Hour").SetTraffic(1000).
		SetQuota(1).SetRenewalReset(true).SaveX(ctx)
	logic := NewActivateOrderLogic(service)
	activate := func(current *ent.Order) {
		t.Helper()
		task := asynq.NewTask("activate", []byte(`{"order_no":"`+current.OrderNo+`"}`))
		for attempt := range 2 {
			if err := logic.ProcessTask(ctx, task); err != nil {
				t.Fatalf("activate %s, attempt %d: %v", current.OrderNo, attempt+1, err)
			}
		}
		if state := db.Order.GetX(ctx, current.ID).Status; state != order.StatusFinished {
			t.Fatalf("%s status = %d, want finished", current.OrderNo, state)
		}
	}
	purchase := db.Order.Create().SetOrderNo("paid-purchase").SetUserID(buyer.ID).SetSubscribeID(plan.ID).
		SetType(order.TypeSubscribe).SetStatus(order.StatusPaid).SetQuantity(24).SaveX(ctx)
	activate(purchase)
	subscription := db.UserSubscribe.Query().OnlyX(ctx)
	if subscription.UserID != buyer.ID || subscription.OrderID != purchase.ID || subscription.SubscribeID != plan.ID ||
		subscription.Status != 1 || subscription.Traffic != 1000 || subscription.Token == "" ||
		subscription.ExpireTime.Sub(subscription.StartTime) != 24*time.Hour {
		t.Fatalf("purchased subscription = %+v", subscription)
	}
	first := db.SubscriptionPeriod.GetX(ctx, entitlement.LocalPeriodID(purchase.OrderNo))
	if first.UserSubscribeID != subscription.ID || first.OrderID != purchase.ID || !first.EndAt.Equal(subscription.ExpireTime) || first.TrafficReset {
		t.Fatalf("purchase period = %+v", first)
	}

	db.UserSubscribe.UpdateOneID(subscription.ID).SetDownload(300).SetUpload(200).SetExpiredDownload(25).SetExpiredUpload(50).SaveX(ctx)
	renewal := db.Order.Create().SetOrderNo("paid-renewal").SetUserID(buyer.ID).SetSubscribeID(plan.ID).
		SetUserSubscribeID(subscription.ID).SetType(order.TypeRenewal).SetStatus(order.StatusPaid).SetQuantity(48).SaveX(ctx)
	activate(renewal)
	renewed := db.UserSubscribe.GetX(ctx, subscription.ID)
	wantExpiry := subscription.ExpireTime.Add(48 * time.Hour)
	if !renewed.ExpireTime.Equal(wantExpiry) || renewed.Download != 0 || renewed.Upload != 0 || renewed.ExpiredDownload != 0 || renewed.ExpiredUpload != 0 {
		t.Fatalf("renewed subscription = %+v, want expiry %v with cleared usage", renewed, wantExpiry)
	}
	second := db.SubscriptionPeriod.GetX(ctx, entitlement.LocalPeriodID(renewal.OrderNo))
	if second.OrderID != renewal.ID || second.UserSubscribeID != subscription.ID || !second.StartAt.Equal(first.EndAt) || !second.EndAt.Equal(wantExpiry) || !second.TrafficReset {
		t.Fatalf("renewal period = %+v", second)
	}

	db.UserSubscribe.UpdateOneID(subscription.ID).SetDownload(400).SetUpload(350).SaveX(ctx)
	reset := db.Order.Create().SetOrderNo("paid-reset").SetUserID(buyer.ID).SetSubscribeID(plan.ID).
		SetUserSubscribeID(subscription.ID).SetType(order.TypeResetTraffic).SetStatus(order.StatusPaid).SaveX(ctx)
	activate(reset)
	cleared := db.UserSubscribe.GetX(ctx, subscription.ID)
	if cleared.Download != 0 || cleared.Upload != 0 || cleared.Traffic != 1000 || !cleared.ExpireTime.Equal(wantExpiry) {
		t.Fatalf("reset subscription = %+v", cleared)
	}
	if count := db.SubscriptionPeriod.Query().CountX(ctx); count != 2 {
		t.Fatalf("entitlement periods = %d, want 2", count)
	}
	audit := db.SystemLog.Query().Where(systemlog.Type(log.TypeResetSubscribe.Uint8())).OnlyX(ctx)
	var event log.ResetSubscribe
	if err := event.Unmarshal([]byte(audit.Content)); err != nil || audit.ObjectID != subscription.ID ||
		event.Type != log.ResetSubscribeTypePaid || event.UserId != buyer.ID || event.OrderNo != reset.OrderNo {
		t.Fatalf("traffic reset audit = %+v, %v", audit, err)
	}
}
