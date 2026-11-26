package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/ticket"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestSupportReplyAndStatusRecoverAsOneConversation(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	member, customer := f.account(false)
	_, support := f.account(true)
	thread := f.client.Ticket.Create().SetUserID(member.ID).SetTitle("May statement").SaveX(ctx)
	var blockedRead, blockedWrite string
	outage := errors.New("conversation store temporarily unavailable")
	for entity, intercept := range map[string]func(...ent.Interceptor){ent.TypeTicket: f.client.Ticket.Intercept, ent.TypeTicketFollow: f.client.TicketFollow.Intercept} {
		intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
			return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
				if blockedRead == entity {
					return nil, outage
				}
				return next.Query(ctx, q)
			})
		}))
	}
	f.client.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if m.Type() == blockedWrite {
				return nil, outage
			}
			return next.Mutate(ctx, m)
		})
	})
	detail := fmt.Sprintf("/detail?id=%d", thread.ID)
	for _, author := range []struct {
		prefix, token, from, text string
		status                    uint8
	}{
		{"/v1/admin/ticket", support, "Billing", "Your May statement is ready", ticket.Waiting},
		{"/v1/public/ticket", customer, "User", "Please include tax", ticket.Pending},
	} {
		for _, blockedRead = range []string{ent.TypeTicket, ent.TypeTicketFollow} {
			require.Empty(t, f.request(http.MethodGet, author.prefix+detail, author.token, "", xerr.DatabaseQueryError))
		}
		body := fmt.Sprintf(`{"ticket_id":"%d","from":%q,"type":1,"content":%q}`, thread.ID, author.from, author.text)
		blockedRead = ent.TypeTicket
		require.Empty(t, f.request(http.MethodPost, author.prefix+"/follow", author.token, body, xerr.DatabaseQueryError))
		blockedRead = ""
		prior := f.client.Ticket.GetX(ctx, thread.ID)
		count := f.client.TicketFollow.Query().CountX(ctx)
		for _, blockedWrite = range []string{ent.TypeTicketFollow, ent.TypeTicket} {
			require.Empty(t, f.request(http.MethodPost, author.prefix+"/follow", author.token, body, xerr.ERROR))
			require.Equal(t, count, f.client.TicketFollow.Query().CountX(ctx))
			require.Equal(t, prior.Status, f.client.Ticket.GetX(ctx, thread.ID).Status)
		}
		blockedWrite = ""
		f.request(http.MethodPost, author.prefix+"/follow", author.token, body, http.StatusOK)
		for _, view := range []struct{ prefix, token string }{{"/v1/admin/ticket", support}, {"/v1/public/ticket", customer}} {
			var updated types.Ticket
			require.NoError(t, json.Unmarshal(f.request(http.MethodGet, view.prefix+detail, view.token, "", http.StatusOK), &updated))
			require.EqualValues(t, author.status, updated.Status)
			require.Len(t, updated.Follows, count+1)
			require.Equal(t, author.from, updated.Follows[count].From)
			require.Equal(t, author.text, updated.Follows[count].Content)
		}
	}
}

func TestCustomerCanReopenSupportConversation(t *testing.T) {
	f := newRouteFixture(t)
	owner, customer := f.account(false)
	_, other := f.account(false)
	_, support := f.account(true)
	const inbox = "/v1/public/ticket"
	const desk = "/v1/admin/ticket"
	f.request(http.MethodPost, inbox+"/", customer, `{"title":"Invoice copy","description":"Please send my May invoice"}`, http.StatusOK)
	f.request(http.MethodPost, inbox+"/", other, `{"title":"Invoice copy","description":"April invoice"}`, http.StatusOK)
	var assigned types.GetTicketListResponse
	query := fmt.Sprintf("%s/list?user_id=%d&search=Invoice&page=1&size=10", desk, owner.ID)
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, query, support, "", http.StatusOK), &assigned))
	require.EqualValues(t, 1, assigned.Total)
	require.Len(t, assigned.List, 1)
	thread := assigned.List[0]
	require.Equal(t, owner.ID, thread.UserId)
	require.Equal(t, "Please send my May invoice", thread.Description)
	detail := fmt.Sprintf("/detail?id=%d", thread.Id)
	f.request(http.MethodGet, inbox+detail, other, "", xerr.InvalidAccess)
	f.request(http.MethodPost, desk+"/follow", support, fmt.Sprintf(`{"ticket_id":"%d","from":"Billing","type":1,"content":"Your invoice is ready"}`, thread.Id), http.StatusOK)
	read := func(path, token string) types.Ticket {
		t.Helper()
		var result types.Ticket
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path+detail, token, "", http.StatusOK), &result))
		return result
	}
	answered := read(inbox, customer)
	require.EqualValues(t, ticket.Waiting, answered.Status)
	require.Len(t, answered.Follows, 1)
	require.Equal(t, "Billing", answered.Follows[0].From)
	require.Equal(t, "Your invoice is ready", answered.Follows[0].Content)
	f.request(http.MethodPut, inbox+"/", customer, fmt.Sprintf(`{"id":"%d","status":%d}`, thread.Id, ticket.Closed), http.StatusOK)
	var open types.GetUserTicketListResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, inbox+"/list", customer, "", http.StatusOK), &open))
	require.Zero(t, open.Total)
	require.Empty(t, open.List)
	require.EqualValues(t, ticket.Closed, read(desk, support).Status)

	f.request(http.MethodPost, inbox+"/follow", customer, fmt.Sprintf(`{"ticket_id":"%d","type":1,"content":"Please include the tax receipt"}`, thread.Id), http.StatusOK)
	reopened := read(desk, support)
	require.EqualValues(t, ticket.Pending, reopened.Status)
	require.Len(t, reopened.Follows, 2)
	require.Equal(t, answered.Follows[0], reopened.Follows[0])
	require.Equal(t, "User", reopened.Follows[1].From)
	require.Equal(t, "Please include the tax receipt", reopened.Follows[1].Content)
	require.Equal(t, reopened.Follows, read(inbox, customer).Follows)
}
