package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	framework "entgo.io/ent"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/systemlog"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestRefundQuoteSettlesOriginalTenderAndReversesEarnedCommissionsOnce(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	buyer, session := f.account(false)
	first, _ := f.account(false)
	second, _ := f.account(false)
	wallet := f.client.UserWallet.Create().SetUserID(buyer.ID).SetBalance(50).SetGiftAmount(10).SaveX(ctx)
	purchaseCommission := f.client.UserWallet.Create().SetUserID(first.ID).SetCommission(90).SaveX(ctx)
	renewalCommission := f.client.UserWallet.Create().SetUserID(second.ID).SetCommission(80).SaveX(ctx)
	plan := f.client.Subscribe.Create().SetName("Metered traffic").SetUnitTime("NoLimit").SetResetCycle(0).SetAllowDeduction(true).SaveX(ctx)
	purchase := f.client.Order.Create().SetOrderNo("refund-purchase").SetUserID(buyer.ID).SetSubscribeID(plan.ID).
		SetType(order.TypeSubscribe).SetQuantity(1).SetStatus(order.StatusFinished).SetAmount(900).SetGiftAmount(100).
		SetCommission(90).SetCommissionRefererID(first.ID).SaveX(ctx)
	f.client.Order.Create().SetOrderNo("refund-renewal").SetUserID(buyer.ID).SetSubscribeID(plan.ID).SetParentID(purchase.ID).
		SetType(order.TypeRenewal).SetQuantity(1).SetStatus(order.StatusFinished).SetAmount(800).SetGiftAmount(200).
		SetCommission(80).SetCommissionRefererID(second.ID).SaveX(ctx)
	f.client.Order.Create().SetOrderNo("unpaid-renewal").SetUserID(buyer.ID).SetSubscribeID(plan.ID).SetParentID(purchase.ID).
		SetType(order.TypeRenewal).SetQuantity(1).SetStatus(order.StatusPending).SetAmount(1200).SaveX(ctx)
	sub := f.client.UserSubscribe.Create().SetToken("refund-subscription").SetOrderID(purchase.ID).SetUserID(buyer.ID).
		SetSubscribeID(plan.ID).SetStatus(1).SetTraffic(1000).SetDownload(350).SetUpload(150).SaveX(ctx)

	const path = "/v1/public/user/unsubscribe"
	body := fmt.Sprintf(`{"id":"%d"}`, sub.ID)
	var offlineEntity string
	var readNumber, reads int
	f.client.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
			if framework.QueryFromContext(ctx).Type == offlineEntity {
				reads++
				if reads == readNumber {
					return nil, errors.New("refund ledger unavailable")
				}
			}
			return next.Query(ctx, q)
		})
	}))
	checkUnsettled := func() {
		t.Helper()
		current := f.client.UserSubscribe.GetX(ctx, sub.ID)
		require.Equal(t, sub.Status, current.Status)
		require.Nil(t, current.FinishedAt)
		saved := f.client.UserWallet.GetX(ctx, wallet.ID)
		require.Equal(t, wallet.Balance, saved.Balance)
		require.Equal(t, wallet.GiftAmount, saved.GiftAmount)
		require.Equal(t, purchaseCommission.Commission, f.client.UserWallet.GetX(ctx, purchaseCommission.ID).Commission)
		require.Equal(t, renewalCommission.Commission, f.client.UserWallet.GetX(ctx, renewalCommission.ID).Commission)
		require.Zero(t, f.client.SystemLog.Query().CountX(ctx))
	}
	for _, fault := range []struct {
		entity string
		read   int
	}{{ent.TypeSubscribe, 1}, {ent.TypeOrder, 1}, {ent.TypeOrder, 2}} {
		offlineEntity, readNumber, reads = fault.entity, fault.read, 0
		require.Empty(t, f.request(http.MethodPost, path+"/pre", session, body, xerr.DatabaseQueryError))
		require.Equal(t, fault.read, reads)
		reads = 0
		require.Empty(t, f.request(http.MethodPost, path, session, body, xerr.ERROR))
		require.Equal(t, fault.read, reads)
		offlineEntity = ""
		checkUnsettled()
	}
	var quote types.PreUnsubscribeResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, path+"/pre", session, body, http.StatusOK), &quote))
	require.EqualValues(t, 1000, quote.DeductionAmount)
	require.EqualValues(t, 1, f.client.UserSubscribe.GetX(ctx, sub.ID).Status)
	require.EqualValues(t, 50, f.client.UserWallet.GetX(ctx, wallet.ID).Balance)
	var entity string
	var nth, hits int
	f.client.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if m.Type() == entity {
				hits++
				if hits == nth {
					return nil, errors.New("refund storage unavailable")
				}
			}
			return next.Mutate(ctx, m)
		})
	})
	for _, fault := range []struct {
		table  string
		writes int
	}{{ent.TypeUserSubscribe, 1}, {ent.TypeUserWallet, 3}, {ent.TypeSystemLog, 4}} {
		entity = fault.table
		for nth = 1; nth <= fault.writes; nth++ {
			hits = 0
			require.Empty(t, f.request(http.MethodPost, path, session, body, xerr.ERROR))
			require.Equal(t, nth, hits, "%s write", entity)
			checkUnsettled()
		}
	}
	entity = ""
	f.request(http.MethodPost, path, session, body, http.StatusOK)
	f.request(http.MethodPost, path, session, body, xerr.ERROR)
	refunded := f.client.UserWallet.GetX(ctx, wallet.ID)
	require.EqualValues(t, 750, refunded.Balance)
	require.EqualValues(t, 310, refunded.GiftAmount)
	require.EqualValues(t, 45, f.client.UserWallet.GetX(ctx, purchaseCommission.ID).Commission)
	require.EqualValues(t, 40, f.client.UserWallet.GetX(ctx, renewalCommission.ID).Commission)
	canceled := f.client.UserSubscribe.GetX(ctx, sub.ID)
	require.EqualValues(t, 4, canceled.Status)
	require.NotNil(t, canceled.FinishedAt)
	entries := f.client.SystemLog.Query().Where(systemlog.Type(log.TypeCommission.Uint8())).AllX(ctx)
	debits := map[int64]int64{}
	for _, entry := range entries {
		var reversal log.Commission
		require.NoError(t, json.Unmarshal([]byte(entry.Content), &reversal))
		require.Equal(t, purchase.OrderNo, reversal.OrderNo)
		debits[entry.ObjectID] += reversal.Amount
	}
	require.Equal(t, map[int64]int64{first.ID: -45, second.ID: -40}, debits)
}
