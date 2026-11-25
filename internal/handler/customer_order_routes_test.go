package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/systemlog"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	worker "github.com/perfect-panel/server/queue/logic/order"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/stretchr/testify/require"
)

func TestAssistedPurchaseRetriesWithoutDuplicatingInventoryOrAudit(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	admin, operator := f.account(true)
	buyer, member := f.account(false)
	plan := f.client.Subscribe.Create().SetName("Assisted purchase").SetSell(true).SetInventory(1).SaveX(ctx)
	method := f.client.Payment.Create().SetName("Bank transfer").SetPlatform("EPay").SetConfig(`{}`).SetEnable(true).SaveX(ctx)
	params := types.CreateOrderRequest{UserId: buyer.ID, Type: order.TypeSubscribe, SubscribeId: plan.ID, PaymentId: method.ID, Quantity: 2, Price: 800, Discount: 50, FeeAmount: 10, Amount: 760}
	payload, err := json.Marshal(params)
	require.NoError(t, err)
	blocked := true
	f.client.SystemLog.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if blocked {
				return nil, errors.New("audit storage unavailable")
			}
			return next.Mutate(ctx, m)
		})
	})
	const create = "/v1/admin/order/"
	f.request(http.MethodPost, create, operator, string(payload), xerr.DatabaseInsertError)
	require.EqualValues(t, 1, f.client.Subscribe.GetX(ctx, plan.ID).Inventory)
	require.Zero(t, f.client.Order.Query().CountX(ctx))
	require.Zero(t, f.client.SystemLog.Query().CountX(ctx))
	blocked = false
	f.request(http.MethodPost, create, operator, string(payload), http.StatusOK)
	require.Zero(t, f.client.Subscribe.GetX(ctx, plan.ID).Inventory)
	var ledger types.GetOrderListResponse
	filter := fmt.Sprintf("list?page=1&size=5&user_id=%d&subscribe_id=%d&status=%d", buyer.ID, plan.ID, order.StatusPending)
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, create+filter, operator, "", http.StatusOK), &ledger))
	require.EqualValues(t, 1, ledger.Total)
	require.Len(t, ledger.List, 1)
	invoice := ledger.List[0]
	var receipt types.OrderDetail
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/order/detail?order_no="+invoice.OrderNo, member, "", http.StatusOK), &receipt))
	require.Equal(t, buyer.ID, receipt.UserId)
	require.Equal(t, plan.Name, receipt.Subscribe.Name)
	require.Equal(t, method.Name, receipt.Payment.Name)
	require.Equal(t, [4]int64{800, 50, 10, 760}, [4]int64{receipt.Price, receipt.Discount, receipt.FeeAmount, receipt.Amount})
	entry := f.client.SystemLog.Query().Where(systemlog.TypeEQ(log.TypeOrderCreated.Uint8())).OnlyX(ctx)
	var audit log.OrderCreated
	require.NoError(t, json.Unmarshal([]byte(entry.Content), &audit))
	require.Equal(t, buyer.ID, entry.ObjectID)
	require.Equal(t, admin.ID, audit.ActorID)
	require.Equal(t, "admin", audit.Source)
	require.Equal(t, invoice.OrderNo, audit.OrderNo)
	require.Equal(t, receipt.Amount, audit.Amount)
}

func TestPurchaseHistoryRecoversWhenThePlanCatalogReturns(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	owner, member := f.account(false)
	other, _ := f.account(false)
	_, operator := f.account(true)
	plan := f.client.Subscribe.Create().SetName("Monthly travel pass").SaveX(ctx)
	method := f.client.Payment.Create().SetName("Travel card").SetPlatform("Stripe").SetConfig(`{}`).SaveX(ctx)
	createPurchase := func(userID int64, number string) (*ent.Order, *ent.UserSubscribe) {
		t.Helper()
		invoice := f.client.Order.Create().SetOrderNo(number).SetUserID(userID).SetSubscribeID(plan.ID).
			SetPaymentID(method.ID).SetType(order.TypeSubscribe).SetAmount(875).SetCommission(25).SetStatus(order.StatusFinished).SaveX(ctx)
		sub := f.client.UserSubscribe.Create().SetUserID(userID).SetOrderID(invoice.ID).SetSubscribeID(plan.ID).
			SetToken(number+"-subscription").SetExpireTime(time.Now().Add(24*time.Hour)).SetStatus(1).SetDownload(325).SaveX(ctx)
		return invoice, sub
	}
	invoice, sub := createPurchase(owner.ID, "buyer-receipt")
	createPurchase(other.ID, "second-receipt")
	var unavailable bool
	f.client.Subscribe.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
			if unavailable {
				return nil, errors.New("plan catalog temporarily unavailable")
			}
			return next.Query(ctx, q)
		})
	}))
	const page = "?page=1&size=10"
	filter := fmt.Sprintf("%s&user_id=%d", page, owner.ID)
	read := func(path, token string, dst any) {
		t.Helper()
		unavailable = true
		require.Empty(t, f.request(http.MethodGet, path, token, "", xerr.DatabaseQueryError))
		unavailable = false
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path, token, "", http.StatusOK), dst))
	}
	var history types.QueryOrderListResponse
	read("/v1/public/order/list"+page, member, &history)
	require.EqualValues(t, 1, history.Total)
	require.Len(t, history.List, 1)
	var detail types.OrderDetail
	read("/v1/public/order/detail?order_no="+invoice.OrderNo, member, &detail)
	require.Equal(t, history.List[0], detail)
	require.Equal(t, invoice.OrderNo, detail.OrderNo)
	require.Equal(t, invoice.Amount, detail.Amount)
	require.Equal(t, method.Name, detail.Payment.Name)
	require.Equal(t, plan.Name, detail.Subscribe.Name)
	require.Zero(t, detail.Commission)
	var ledger types.GetOrderListResponse
	read("/v1/admin/order/list"+filter, operator, &ledger)
	require.EqualValues(t, 1, ledger.Total)
	require.Len(t, ledger.List, 1)
	require.Equal(t, invoice.ID, ledger.List[0].Id)
	require.Equal(t, invoice.Commission, ledger.List[0].Commission)
	var subscriptions types.QueryUserSubscribeListResponse
	read("/v1/public/user/subscribe", member, &subscriptions)
	require.EqualValues(t, 1, subscriptions.Total)
	require.Len(t, subscriptions.List, 1)
	require.Equal(t, sub.ID, subscriptions.List[0].Id)
	require.Equal(t, invoice.ID, subscriptions.List[0].OrderId)
	require.Equal(t, plan.Name, subscriptions.List[0].Subscribe.Name)
	var managed types.GetUserSubscribeListResponse
	read("/v1/admin/user/subscribe"+filter, operator, &managed)
	require.Len(t, managed.List, 1)
	require.Equal(t, sub.ID, managed.List[0].Id)
	var grant types.UserSubscribeDetail
	read(fmt.Sprintf("/v1/admin/user/subscribe/detail?id=%d", sub.ID), operator, &grant)
	require.Equal(t, owner.ID, grant.User.Id)
	require.Equal(t, invoice.ID, grant.OrderId)
	require.Equal(t, sub.Download, grant.Download)
}

func TestRejectedPurchaseAndRenewalDoNotReserveCustomerFunds(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	inspector := f.startQueue()
	owner, session := f.account(false)
	wallet := f.client.UserWallet.Create().SetUserID(owner.ID).SetGiftAmount(120).SetBalance(900).SaveX(ctx)
	plan := f.client.Subscribe.Create().SetName("Annual pass").SetSell(true).SetInventory(5).SetUnitPrice(750).SaveX(ctx)
	sub := f.client.UserSubscribe.Create().SetUserID(owner.ID).SetOrderID(1).SetSubscribeID(plan.ID).SetToken("annual-pass").SetExpireTime(time.Now().Add(time.Hour)).SetStatus(1).SaveX(ctx)
	method := f.client.Payment.Create().SetPlatform("Stripe").SetEnable(true).SetConfig(`{}`).SaveX(ctx)
	voucher := f.client.Coupon.Create().SetCode("ANNUAL").SetType(2).SetDiscount(100).SetCount(1).SetExpireTime(time.Now().Add(time.Hour).UnixMilli()).SaveX(ctx)
	request := func(route string, paymentID int64, coupon string, code uint32) json.RawMessage {
		t.Helper()
		field, id := "subscribe_id", plan.ID
		if route == "renewal" {
			field, id = "user_subscribe_id", sub.ID
		}
		body := fmt.Sprintf(`{%q:"%d","payment":"%d","quantity":1,"coupon":%q}`, field, id, paymentID, coupon)
		return f.request(http.MethodPost, "/v1/public/order/"+route, session, body, code)
	}
	for _, route := range []string{"pre", "purchase", "renewal"} {
		request(route, method.ID, "unissued", xerr.CouponNotExist)
		failure := xerr.PaymentMethodNotFound
		if route == "pre" {
			failure = xerr.DatabaseQueryError
		}
		request(route, method.ID+100, voucher.Code, failure)
	}
	f.client.Coupon.UpdateOneID(voucher.ID).SetUsedCount(1).ExecX(ctx)
	request("pre", method.ID, voucher.Code, xerr.CouponAlreadyUsed)
	for _, route := range []string{"purchase", "renewal"} {
		request(route, method.ID, voucher.Code, xerr.CouponInsufficientUsage)
	}
	f.client.Subscribe.UpdateOneID(plan.ID).SetSell(false).ExecX(ctx)
	for _, route := range []string{"purchase", "renewal"} {
		request(route, method.ID, "", xerr.SubscribeNotAvailable)
	}
	require.Zero(t, f.client.Order.Query().CountX(ctx))
	require.Zero(t, f.client.SystemLog.Query().CountX(ctx))
	require.EqualValues(t, 1, f.client.Coupon.GetX(ctx, voucher.ID).UsedCount)
	require.EqualValues(t, 5, f.client.Subscribe.GetX(ctx, plan.ID).Inventory)
	funds := f.client.UserWallet.GetX(ctx, wallet.ID)
	require.Equal(t, wallet.GiftAmount, funds.GiftAmount)
	require.Equal(t, wallet.Balance, funds.Balance)
	queues, err := inspector.Queues()
	require.NoError(t, err)
	require.Empty(t, queues)
	f.client.Subscribe.UpdateOneID(plan.ID).SetSell(true).ExecX(ctx)
	var accepted types.RenewalOrderResponse
	require.NoError(t, json.Unmarshal(request("renewal", method.ID, "", http.StatusOK), &accepted))
	invoice, err := f.service.Store.Order().FindOneByOrderNo(ctx, accepted.OrderNo)
	require.NoError(t, err)
	require.Equal(t, sub.ID, invoice.UserSubscribeId)
	require.EqualValues(t, 630, invoice.Amount)
	require.EqualValues(t, 120, invoice.GiftAmount)
}

func TestQuotesAndOrdersRecoverWhenBillingInputsBecomeReadable(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	queueInspector := f.startQueue()
	member, token := f.account(false)
	wallet := f.client.UserWallet.Create().SetUserID(member.ID).SetBalance(1200).SetGiftAmount(100).SaveX(ctx)
	plan := f.client.Subscribe.Create().SetName("Quarterly").SetSell(true).SetUnitPrice(500).SetInventory(2).SetQuota(2).SaveX(ctx)
	coupon := f.client.Coupon.Create().SetCode("SPRING").SetCount(1).SetType(2).SetDiscount(80).
		SetExpireTime(time.Now().Add(time.Hour).UnixMilli()).SaveX(ctx)
	method := f.client.Payment.Create().SetPlatform("Stripe").SetEnable(true).SetConfig(`{}`).SetFeeMode(2).SetFeeAmount(30).SaveX(ctx)
	body := fmt.Sprintf(`{"subscribe_id":"%d","payment":"%d","coupon":"SPRING","quantity":2}`, plan.ID, method.ID)
	var unavailable string
	f.client.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, query ent.Query) (ent.Value, error) {
			var source string
			switch query.(type) {
			case *ent.SubscribeQuery:
				source = "plan"
			case *ent.PaymentQuery:
				source = "payment method"
			case *ent.CouponQuery:
				source = "coupon"
			case *ent.OrderQuery:
				source = "order history"
			case *ent.UserSubscribeQuery:
				source = "subscription quota"
			case *ent.UserWalletQuery:
				source = "wallet"
			}
			if unavailable != "" && source == unavailable {
				return nil, errors.New("billing input is unavailable")
			}
			return next.Query(ctx, query)
		})
	}))
	const memberQuote = "/v1/public/order/pre"
	const guestQuote = "/v1/public/portal/pre"
	const purchase = "/v1/public/order/purchase"
	for _, unavailable = range []string{"plan", "payment method", "coupon", "order history", "subscription quota", "wallet"} {
		require.Empty(t, f.request(http.MethodPost, memberQuote, token, body, xerr.DatabaseQueryError), unavailable)
		if unavailable == "plan" || unavailable == "payment method" || unavailable == "coupon" {
			require.Empty(t, f.request(http.MethodPost, guestQuote, "", body, xerr.DatabaseQueryError), unavailable)
		}
		code := xerr.DatabaseQueryError
		if unavailable == "wallet" {
			code = xerr.DatabaseInsertError
		}
		require.Empty(t, f.request(http.MethodPost, purchase, token, body, code), unavailable)
	}
	unavailable = ""
	require.Zero(t, f.client.Order.Query().CountX(ctx))
	require.Zero(t, f.client.OrderEvent.Query().CountX(ctx))
	require.Zero(t, f.client.SystemLog.Query().CountX(ctx))
	require.EqualValues(t, 2, f.client.Subscribe.GetX(ctx, plan.ID).Inventory)
	require.Zero(t, f.client.Coupon.GetX(ctx, coupon.ID).UsedCount)
	require.EqualValues(t, 100, f.client.UserWallet.GetX(ctx, wallet.ID).GiftAmount)
	queues, err := queueInspector.Queues()
	require.NoError(t, err)
	require.Empty(t, queues)
	var guest types.PrePurchaseOrderResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, guestQuote, "", body, http.StatusOK), &guest))
	require.EqualValues(t, 950, guest.Amount)
	var quote types.PreOrderResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, memberQuote, token, body, http.StatusOK), &quote))
	require.EqualValues(t, 100, quote.GiftAmount)
	require.EqualValues(t, 850, quote.Amount)
	var placed types.PurchaseOrderResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, purchase, token, body, http.StatusOK), &placed))
	invoice := f.client.Order.Query().OnlyX(ctx)
	require.Equal(t, placed.OrderNo, invoice.OrderNo)
	require.Equal(t, quote.Amount, invoice.Amount)
	require.Equal(t, quote.GiftAmount, invoice.GiftAmount)
	require.EqualValues(t, 1, f.client.Subscribe.GetX(ctx, plan.ID).Inventory)
	require.EqualValues(t, 1, f.client.Coupon.GetX(ctx, coupon.ID).UsedCount)
	funds := f.client.UserWallet.GetX(ctx, wallet.ID)
	require.Equal(t, wallet.Balance, funds.Balance)
	require.Zero(t, funds.GiftAmount)
}

func TestLegacyCheckoutFulfillsPurchaseCoveredByReservedGift(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	inspector := f.startQueue()
	owner, session := f.account(false)
	_, other := f.account(false)
	wallet := f.client.UserWallet.Create().SetUserID(owner.ID).SetBalance(200).SetGiftAmount(800).SaveX(ctx)
	plan := f.client.Subscribe.Create().SetName("Monthly").SetUnitPrice(500).SetUnitTime("Day").SetSell(true).SetInventory(1).SaveX(ctx)
	method := f.client.Payment.Create().SetPlatform("Stripe").SetConfig("invalid gateway config").SetEnable(true).
		SetFeeMode(3).SetFeePercent(3).SetFeeAmount(30).SaveX(ctx)
	var created types.PurchaseOrderResponse
	purchase := fmt.Sprintf(`{"subscribe_id":"%d","quantity":1,"payment":"%d"}`, plan.ID, method.ID)
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, "/v1/public/order/purchase", session, purchase, http.StatusOK), &created))
	const checkoutPath = "/v1/public/portal/order/checkout"
	checkout := fmt.Sprintf(`{"orderNo":%q}`, created.OrderNo)
	for _, token := range []string{"", other} {
		f.request(http.MethodPost, checkoutPath, token, checkout, xerr.InvalidAccess)
	}
	invoice := f.client.Order.Query().OnlyX(ctx)
	require.Equal(t, order.StatusPending, invoice.Status)
	require.Zero(t, invoice.Amount)
	require.EqualValues(t, 545, invoice.GiftAmount)

	var result types.CheckoutOrderResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, checkoutPath, session, checkout, http.StatusOK), &result))
	require.Equal(t, "balance", result.Type)
	require.Empty(t, result.CheckoutUrl)
	require.Equal(t, order.StatusPaid, f.client.Order.GetX(ctx, invoice.ID).Status)
	tasks, err := inspector.ListPendingTasks("default")
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, queue.ForthwithActivateOrder, tasks[0].Type)
	require.NoError(t, worker.NewActivateOrderLogic(f.service).ProcessTask(ctx, asynq.NewTask(tasks[0].Type, tasks[0].Payload)))

	f.request(http.MethodPost, checkoutPath, session, checkout, http.StatusOK)
	require.Equal(t, order.StatusFinished, f.client.Order.GetX(ctx, invoice.ID).Status)
	require.Equal(t, invoice.ID, f.client.UserSubscribe.Query().OnlyX(ctx).OrderID)
	funds := f.client.UserWallet.GetX(ctx, wallet.ID)
	require.EqualValues(t, 200, funds.Balance)
	require.EqualValues(t, 255, funds.GiftAmount)
}

func TestMemberOrderCanRetryAfterBillingStorageRecovers(t *testing.T) {
	for _, tc := range []struct {
		path    string
		kind    uint8
		amount  int64
		gift    int64
		uses    int64
		stock   int64
		failure uint32
	}{
		{"purchase", order.TypeSubscribe, 805, 125, 1, 3, xerr.DatabaseInsertError},
		{"renewal", order.TypeRenewal, 805, 125, 1, 4, xerr.ERROR},
		{"reset", order.TypeResetTraffic, 255, 125, 0, 4, xerr.ERROR},
		{"recharge", order.TypeRecharge, 530, 0, 0, 4, xerr.ERROR},
	} {
		t.Run(tc.path, func(t *testing.T) {
			f := newRouteFixture(t)
			ctx := t.Context()
			inspector := f.startQueue()
			owner, session := f.account(false)
			wallet := f.client.UserWallet.Create().SetUserID(owner.ID).SetBalance(900).SetGiftAmount(125).SaveX(ctx)
			plan := f.client.Subscribe.Create().SetName("Monthly").SetSell(true).SetInventory(4).SetUnitPrice(500).SetReplacement(350).SaveX(ctx)
			parent := f.client.Order.Create().SetOrderNo("existing-plan").SetUserID(owner.ID).SetSubscribeID(plan.ID).SetStatus(order.StatusFinished).SaveX(ctx)
			sub := f.client.UserSubscribe.Create().SetUserID(owner.ID).SetOrderID(parent.ID).SetSubscribeID(plan.ID).SetStatus(1).
				SetToken("existing-subscription").SetExpireTime(time.Now().Add(time.Hour)).SetTraffic(1000).SetDownload(230).SaveX(ctx)
			voucher := f.client.Coupon.Create().SetCode("MEMBER").SetType(2).SetDiscount(100).SetCount(1).SetExpireTime(time.Now().Add(time.Hour).UnixMilli()).SaveX(ctx)
			method := f.client.Payment.Create().SetPlatform("Stripe").SetConfig(`{}`).SetEnable(true).SetFeeMode(2).SetFeeAmount(30).SaveX(ctx)
			var body string
			switch tc.kind {
			case order.TypeSubscribe:
				body = fmt.Sprintf(`{"subscribe_id":"%d","payment":"%d","quantity":2,"coupon":"MEMBER"}`, plan.ID, method.ID)
			case order.TypeRenewal:
				body = fmt.Sprintf(`{"user_subscribe_id":"%d","payment":"%d","quantity":2,"coupon":"MEMBER"}`, sub.ID, method.ID)
			case order.TypeResetTraffic:
				body = fmt.Sprintf(`{"user_subscribe_id":"%d","payment":"%d"}`, sub.ID, method.ID)
			case order.TypeRecharge:
				body = fmt.Sprintf(`{"amount":500,"payment":"%d"}`, method.ID)
			}
			var unavailable string
			var interrupted bool
			f.client.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
					entity := m.Type()
					if entry, ok := m.(*ent.SystemLogMutation); ok {
						if kind, _ := entry.GetType(); kind == log.TypeOrderCreated.Uint8() {
							entity = "creation audit"
						}
					}
					if entity == unavailable {
						interrupted = true
						return nil, fmt.Errorf("%s storage temporarily unavailable", unavailable)
					}
					return next.Mutate(ctx, m)
				})
			})
			writes := []string{ent.TypeOrder, ent.TypeOrderEvent, "creation audit"}
			if tc.gift > 0 {
				writes = append(writes, ent.TypeUserWallet, ent.TypeSystemLog)
			}
			if tc.uses > 0 {
				writes = append(writes, ent.TypeCoupon)
			}
			if tc.kind == order.TypeSubscribe {
				writes = append(writes, ent.TypeSubscribe)
			}
			path := "/v1/public/order/" + tc.path
			for _, unavailable = range writes {
				interrupted = false
				f.request(http.MethodPost, path, session, body, tc.failure)
				require.True(t, interrupted, "expected a %s write", unavailable)
				require.Equal(t, parent.ID, f.client.Order.Query().OnlyX(ctx).ID)
				require.Zero(t, f.client.OrderEvent.Query().CountX(ctx))
				require.Zero(t, f.client.SystemLog.Query().CountX(ctx))
				require.Zero(t, f.client.Coupon.GetX(ctx, voucher.ID).UsedCount)
				require.EqualValues(t, 4, f.client.Subscribe.GetX(ctx, plan.ID).Inventory)
				require.EqualValues(t, 125, f.client.UserWallet.GetX(ctx, wallet.ID).GiftAmount)
				queues, err := inspector.Queues()
				require.NoError(t, err)
				require.Empty(t, queues)
			}

			unavailable = ""
			var created struct {
				OrderNo string `json:"order_no"`
			}
			require.NoError(t, json.Unmarshal(f.request(http.MethodPost, path, session, body, http.StatusOK), &created))
			invoice, err := f.service.Store.Order().FindOneByOrderNo(ctx, created.OrderNo)
			require.NoError(t, err)
			require.Equal(t, tc.kind, invoice.Type)
			require.Equal(t, owner.ID, invoice.UserId)
			require.Equal(t, tc.amount, invoice.Amount)
			require.Equal(t, tc.gift, invoice.GiftAmount)
			require.Equal(t, tc.uses, f.client.Coupon.GetX(ctx, voucher.ID).UsedCount)
			require.Equal(t, tc.stock, f.client.Subscribe.GetX(ctx, plan.ID).Inventory)
			funds := f.client.UserWallet.GetX(ctx, wallet.ID)
			require.Equal(t, int64(125)-tc.gift, funds.GiftAmount)
			require.EqualValues(t, 900, funds.Balance)
			current := f.client.UserSubscribe.GetX(ctx, sub.ID)
			require.Equal(t, sub.Download, current.Download)
			require.True(t, current.ExpireTime.Equal(sub.ExpireTime))
			event := f.client.OrderEvent.Query().OnlyX(ctx)
			require.Equal(t, created.OrderNo, event.OrderNo)
			require.Equal(t, order.EventOrderCreated, event.EventType)
			entry := f.client.SystemLog.Query().Where(systemlog.TypeEQ(log.TypeOrderCreated.Uint8())).OnlyX(ctx)
			var receipt log.OrderCreated
			require.NoError(t, json.Unmarshal([]byte(entry.Content), &receipt))
			require.Equal(t, owner.ID, entry.ObjectID)
			require.Equal(t, created.OrderNo, receipt.OrderNo)
			require.Equal(t, tc.amount, receipt.Amount)
			require.Equal(t, tc.gift, receipt.GiftAmount)
			tasks, err := inspector.ListScheduledTasks("default")
			require.NoError(t, err)
			require.Len(t, tasks, 1)
			require.Equal(t, queue.DeferCloseOrder, tasks[0].Type)
			var expiry queue.DeferCloseOrderPayload
			require.NoError(t, json.Unmarshal(tasks[0].Payload, &expiry))
			require.Equal(t, created.OrderNo, expiry.OrderNo)
		})
	}
}
