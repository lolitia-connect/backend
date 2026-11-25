package user

import (
	"context"
	"testing"
	"time"

	usermodel "github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/pkg/constant"
)

func TestSubscriptionListShowsRenewalTermsForCurrentAccount(t *testing.T) {
	service, client, _ := newAccountFixture(t)
	ctx := t.Context()
	owner := client.User.Create().SetPassword("fixture").SaveX(ctx)
	other := client.User.Create().SetPassword("fixture").SaveX(ctx)
	plan := client.Subscribe.Create().SetName("Standard").SetResetCycle(1).
		SetDiscount(`[{"quantity":3,"discount":0.8}]`).SaveX(ctx)
	const subscriptionID int64 = 9007199254740993
	future := time.Now().Add(time.Hour)
	client.UserSubscribe.Create().SetID(subscriptionID).SetUserID(owner.ID).SetOrderID(1).
		SetSubscribeID(plan.ID).SetStatus(1).SetToken("owner-subscription").SetExpireTime(future).SaveX(ctx)
	client.UserSubscribe.Create().SetUserID(other.ID).SetOrderID(2).
		SetSubscribeID(plan.ID).SetStatus(1).SetToken("other-subscription").SetExpireTime(future).SaveX(ctx)
	client.UserSubscribe.Create().SetUserID(owner.ID).SetOrderID(3).
		SetSubscribeID(plan.ID).SetStatus(4).SetToken("canceled-subscription").SetExpireTime(future).SaveX(ctx)

	ownerCtx := context.WithValue(ctx, constant.CtxKeyUser, &usermodel.User{Id: owner.ID})
	list, err := NewQueryUserSubscribeLogic(ownerCtx, service).QueryUserSubscribe()
	if err != nil || list == nil || list.Total != 1 || len(list.List) != 1 {
		t.Fatalf("subscription list = %+v, %v", list, err)
	}
	item := list.List[0]
	if item.Id != subscriptionID || item.IdStr != "9007199254740993" || item.Token != "owner-subscription" {
		t.Fatalf("unexpected subscription in current account's list: %d, %q", item.Id, item.IdStr)
	}
	if terms := item.Subscribe.Discount; len(terms) != 1 || terms[0].Quantity != 3 || terms[0].Discount != 0.8 {
		t.Fatalf("renewal discounts = %+v", terms)
	}
	reset := time.UnixMilli(item.ResetTime).In(time.Local)
	if reset.Day() != 1 || reset.Hour() != 0 || reset.Minute() != 0 || !reset.After(future.Add(-time.Hour)) {
		t.Fatalf("scheduled traffic reset = %s, want a future month boundary", reset)
	}
}
