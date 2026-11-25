package orderLogic

import (
	"errors"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	modelorder "github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/model/user"
)

func TestActivateOrderHandlesRechargeStatusesAndNotifications(t *testing.T) {
	service, _ := stripeTestService(t)
	ctx := t.Context()
	account := service.Ent.User.Create().SetPassword("fixture").SaveX(ctx)
	wallet := service.Ent.UserWallet.Create().SetUserID(account.ID).SetBalance(100).SaveX(ctx)
	invoice := service.Ent.Order.Create().SetOrderNo("recharge-activation").SetUserID(account.ID).
		SetType(modelorder.TypeRecharge).SetStatus(modelorder.StatusPaid).SetPrice(750).SetMethod("balance").SaveX(ctx)
	current, err := service.Store.Order().FindOneByOrderNo(ctx, invoice.OrderNo)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewActivateOrderLogic(service).Recharge(ctx, current); err != nil {
		t.Fatal(err)
	}
	if got := service.Ent.UserWallet.GetX(ctx, wallet.ID); got.Balance != 850 {
		t.Fatalf("recharged wallet = %+v", got)
	}
	if service.Ent.SystemLog.Query().CountX(ctx) != 1 {
		t.Fatal("recharge did not write a balance audit")
	}

	logic := NewActivateOrderLogic(service)
	for _, tc := range []struct {
		name   string
		status uint8
		want   error
	}{
		{"finished", modelorder.StatusFinished, nil},
		{"closed", modelorder.StatusClosed, nil},
		{"pending", modelorder.StatusPending, ErrInvalidOrderStatus},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := service.Ent.Order.Create().SetOrderNo("status-" + tc.name).SetUserID(account.ID).
				SetType(modelorder.TypeRecharge).SetStatus(tc.status).SetPrice(1).SaveX(ctx)
			err := logic.ProcessTask(ctx, asynq.NewTask("activate", []byte(`{"order_no":"`+row.OrderNo+`"}`)))
			if !errors.Is(err, tc.want) {
				t.Fatalf("status %s error = %v, want %v", tc.name, err, tc.want)
			}
		})
	}
	apple := service.Ent.Order.Create().SetOrderNo("apple-activation").SetUserID(account.ID).
		SetType(modelorder.TypeRecharge).SetStatus(modelorder.StatusPaid).SetMethod("AppleIAP").SaveX(ctx)
	if err := logic.ProcessTask(ctx, asynq.NewTask("activate", []byte(`{"order_no":"`+apple.OrderNo+`"}`))); !errors.Is(err, user.ErrProviderManaged) {
		t.Fatalf("provider-managed order error = %v", err)
	}
	unknown := service.Ent.Order.Create().SetOrderNo("unknown-activation").SetType(99).SetStatus(modelorder.StatusPaid).SaveX(ctx)
	if err := logic.ProcessTask(ctx, asynq.NewTask("activate", []byte(`{"order_no":"`+unknown.OrderNo+`"}`))); !errors.Is(err, ErrInvalidOrderType) {
		t.Fatalf("unknown order type error = %v", err)
	}
	if err := logic.ProcessTask(ctx, asynq.NewTask("activate", []byte("not-json"))); err == nil {
		t.Fatal("malformed activation payload was accepted")
	}
	if _, err := logic.validateAndGetOrder(ctx, invoice.OrderNo); err != nil {
		t.Fatalf("paid order validation = %v", err)
	}
	if _, err := logic.validateAndGetOrder(ctx, "missing-order"); err == nil {
		t.Fatal("missing order validation succeeded")
	}
}

func TestActivateOrderNotificationDataAndReferralBehavior(t *testing.T) {
	service, _ := stripeTestService(t)
	ctx := t.Context()
	logic := NewActivateOrderLogic(service)
	referrer := service.Ent.User.Create().SetPassword("referrer").SetReferCode("INVITE-FIXTURE").SaveX(ctx)
	buyer := service.Ent.User.Create().SetPassword("buyer").SaveX(ctx)
	buyerModel, err := service.Store.User().FindOne(ctx, buyer.ID)
	if err != nil {
		t.Fatal(err)
	}
	logic.handleReferrer(ctx, buyerModel, referrer.ReferCode)
	updated := service.Ent.User.GetX(ctx, buyer.ID)
	if updated.RefererID != referrer.ID {
		t.Fatalf("referral owner = %d, want %d", updated.RefererID, referrer.ID)
	}
	logic.handleReferrer(ctx, buyerModel, "missing-invite")
	logic.handleReferrer(ctx, buyerModel, "")

	now := time.Now()
	sub := &user.Subscribe{ExpireTime: now.Add(24 * time.Hour)}
	orderInfo := &modelorder.Order{OrderNo: "notify-order", Type: modelorder.TypeSubscribe, Price: 1250, Method: "Stripe", CreatedAt: now}
	data := logic.buildUserNotificationData(orderInfo, &subscribe.Subscribe{Name: "Fixture plan"}, sub)
	if data["OrderNo"] != orderInfo.OrderNo || data["SubscribeName"] != "Fixture plan" || data["OrderAmount"] != "12.50" || data["ExpireTime"] == "" || data["ResetTime"] == "" {
		t.Fatalf("user notification data = %+v", data)
	}
	admin := logic.buildAdminNotificationData(orderInfo, &subscribe.Subscribe{Name: "Fixture plan"}, &user.User{AuthMethods: []user.AuthMethods{{AuthType: "email", AuthIdentifier: "buyer@example.test"}}})
	if admin["UserEmail"] != "buyer@example.test" || admin["SubscribeName"] != "Fixture plan" || admin["OrderStatus"] != "已支付" {
		t.Fatalf("admin notification data = %+v", admin)
	}
	reset := *orderInfo
	reset.Type = modelorder.TypeResetTraffic
	admin = logic.buildAdminNotificationData(&reset, &subscribe.Subscribe{Name: "Fixture plan"}, &user.User{})
	if admin["SubscribeName"] != "流量重置" || admin["UserEmail"] != "" {
		t.Fatalf("reset notification data = %+v", admin)
	}

	identities := []user.AuthMethods{{AuthType: "telegram", AuthIdentifier: "invalid"}, {AuthType: "telegram", AuthIdentifier: "987654"}}
	accountWithMethods := &user.User{AuthMethods: identities}
	if chat, ok := findTelegram(accountWithMethods); !ok || chat != 987654 {
		t.Fatalf("telegram identity = %d, %t", chat, ok)
	}
	if email := findEmail(&user.User{AuthMethods: []user.AuthMethods{{AuthType: "email", AuthIdentifier: "mail@example.test"}}}); email != "mail@example.test" {
		t.Fatalf("email identity = %q", email)
	}
	if email := findEmail(&user.User{}); email != "" {
		t.Fatalf("missing email identity = %q", email)
	}
	if _, err := logic.getSubscribeInfo(ctx, 999999); !errors.Is(err, ErrUnfulfillable) {
		t.Fatalf("missing subscription plan error = %v", err)
	}
	if err := logic.processOrderByType(ctx, &modelorder.Order{Type: 99}); !errors.Is(err, ErrInvalidOrderType) {
		t.Fatalf("invalid process type error = %v", err)
	}
	if got := logic.calculateCommission(1001, 10); got != 100 {
		t.Fatalf("commission = %d, want 100", got)
	}
}
