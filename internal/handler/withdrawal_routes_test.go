package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestCommissionWithdrawalAndReviewRollBackOnStorageFailure(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	member, customer := f.account(false)
	_, reviewer := f.account(true)
	wallet := f.client.UserWallet.Create().SetUserID(member.ID).SetBalance(40).SetCommission(750).SaveX(ctx)
	var failWithdrawal, failReview bool
	f.client.UserWithdrawal.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if failWithdrawal && m.Op().Is(ent.OpCreate) || failReview && m.Op().Is(ent.OpUpdate|ent.OpUpdateOne) {
				return nil, errors.New("payout storage temporarily unavailable")
			}
			return next.Mutate(ctx, m)
		})
	})
	withdraw := func(want uint32) {
		t.Helper()
		f.request(http.MethodPost, "/v1/public/user/commission_withdraw", customer, `{"amount":500,"content":"Fixture payout account"}`, want)
	}
	ledger := func() []int64 {
		t.Helper()
		var result types.QueryUserCommissionLogListResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/user/commission_log?page=1&size=10", customer, "", http.StatusOK), &result))
		var amounts []int64
		for _, entry := range result.List {
			require.Equal(t, member.ID, entry.UserId)
			amounts = append(amounts, entry.Amount)
		}
		return amounts
	}
	checkWallet := func(commission int64) {
		t.Helper()
		got := f.client.UserWallet.GetX(ctx, wallet.ID)
		require.Equal(t, commission, got.Commission)
		require.EqualValues(t, 40, got.Balance)
	}
	failWithdrawal = true
	withdraw(xerr.DatabaseInsertError)
	checkWallet(750)
	require.Empty(t, ledger())
	require.Zero(t, f.client.UserWithdrawal.Query().CountX(ctx))
	failWithdrawal = false
	withdraw(http.StatusOK)
	withdraw(xerr.UserCommissionNotEnough)
	checkWallet(250)
	require.Equal(t, []int64{-500}, ledger())
	var pending types.GetWithdrawalListResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, fmt.Sprintf("/v1/admin/withdrawal/list?user_id=%d&status=0", member.ID), reviewer, "", http.StatusOK), &pending))
	require.Len(t, pending.List, 1)
	id := pending.List[0].Id
	require.Positive(t, id)
	require.EqualValues(t, 500, pending.List[0].Amount)
	review := func(want uint32) {
		t.Helper()
		f.request(http.MethodPut, "/v1/admin/withdrawal/status", reviewer, fmt.Sprintf(`{"id":"%d","status":2,"reason":"  Destination unavailable  "}`, id), want)
	}
	failReview = true
	review(xerr.DatabaseUpdateError)
	checkWallet(250)
	require.Equal(t, []int64{-500}, ledger())
	require.EqualValues(t, types.WithdrawalStatusPending, f.client.UserWithdrawal.GetX(ctx, id).Status)
	failReview = false
	review(http.StatusOK)
	review(xerr.WithdrawalAlreadyReviewed)
	checkWallet(750)
	require.ElementsMatch(t, []int64{-500, 500}, ledger())
	var history types.QueryWithdrawalLogListResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/user/withdrawal_log?page=1&size=10", customer, "", http.StatusOK), &history))
	require.Len(t, history.List, 1)
	require.Equal(t, id, history.List[0].Id)
	require.Equal(t, "Destination unavailable", history.List[0].Reason)
	require.EqualValues(t, types.WithdrawalStatusRejected, history.List[0].Status)
}
