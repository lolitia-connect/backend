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
	var unavailableRead, readOnly string
	outage := errors.New("payout storage temporarily unavailable")
	for entity, intercept := range map[string]func(...ent.Interceptor){ent.TypeUserWallet: f.client.UserWallet.Intercept, ent.TypeUserWithdrawal: f.client.UserWithdrawal.Intercept} {
		intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
			return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
				if entity == unavailableRead {
					return nil, outage
				}
				return next.Query(ctx, q)
			})
		}))
	}
	f.client.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if readOnly == m.Type() {
				return nil, outage
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
	for _, failure := range []struct {
		read, write string
		code        uint32
	}{{ent.TypeUserWallet, "", xerr.DatabaseQueryError}, {"", ent.TypeUserWallet, xerr.DatabaseUpdateError}, {"", ent.TypeSystemLog, xerr.DatabaseInsertError}, {"", ent.TypeUserWithdrawal, xerr.DatabaseInsertError}} {
		unavailableRead, readOnly = failure.read, failure.write
		withdraw(failure.code)
		unavailableRead, readOnly = "", ""
		checkWallet(750)
		require.Empty(t, ledger())
		require.Zero(t, f.client.UserWithdrawal.Query().CountX(ctx))
	}
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
	for _, failure := range []struct {
		read, write string
		code        uint32
	}{
		{ent.TypeUserWithdrawal, "", xerr.DatabaseQueryError},
		{ent.TypeUserWallet, "", xerr.DatabaseQueryError},
		{"", ent.TypeUserWallet, xerr.DatabaseUpdateError},
		{"", ent.TypeSystemLog, xerr.DatabaseInsertError},
		{"", ent.TypeUserWithdrawal, xerr.DatabaseUpdateError},
	} {
		unavailableRead, readOnly = failure.read, failure.write
		review(failure.code)
		unavailableRead, readOnly = "", ""
		checkWallet(250)
		require.Equal(t, []int64{-500}, ledger())
		require.EqualValues(t, types.WithdrawalStatusPending, f.client.UserWithdrawal.GetX(ctx, id).Status)
	}
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
