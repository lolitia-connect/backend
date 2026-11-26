package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	framework "entgo.io/ent"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/internal/usersession"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestMemberSeesOperatorSubscriptionChanges(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	owner, session := f.account(false)
	starter := f.client.Subscribe.Create().SetName("Starter").SaveX(ctx)
	standard := f.client.Subscribe.Create().SetName("Standard").SaveX(ctx)
	const admin = "/v1/admin/user/subscribe"
	const member = "/v1/public/user/subscribe"
	expires := time.Now().AddDate(0, 1, 0).UnixMilli()
	body := fmt.Sprintf(`{"user_id":"%d","subscribe_id":"%d","traffic":1024,"expired_at":%d}`, owner.ID, starter.ID, expires)
	f.service.Config.Subscribe.SingleModel = true
	list := func(path, token string) []types.UserSubscribe {
		t.Helper()
		var result types.GetUserSubscribeListResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path, token, "", http.StatusOK), &result))
		require.EqualValues(t, len(result.List), result.Total)
		return result.List
	}
	operatorView := fmt.Sprintf("%s?user_id=%d&page=1&size=10", admin, owner.ID)
	var readOffline, writeOffline bool
	unavailable := errors.New("subscription storage unavailable")
	f.client.UserSubscribe.Intercept(framework.InterceptFunc(func(next framework.Querier) framework.Querier {
		return framework.QuerierFunc(func(ctx context.Context, q framework.Query) (framework.Value, error) {
			if readOffline {
				return nil, unavailable
			}
			return next.Query(ctx, q)
		})
	}))
	f.client.UserSubscribe.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if writeOffline {
				return nil, unavailable
			}
			return next.Mutate(ctx, m)
		})
	})
	retryChange := func(method, path, token, payload string, writeCode uint32) {
		t.Helper()
		before := list(operatorView, operator)
		readOffline = true
		f.request(method, path, token, payload, xerr.DatabaseQueryError)
		readOffline = false
		require.Equal(t, before, list(operatorView, operator))
		writeOffline = true
		f.request(method, path, token, payload, writeCode)
		writeOffline = false
		require.Equal(t, before, list(operatorView, operator))
		f.request(method, path, token, payload, http.StatusOK)
	}
	retryChange(http.MethodPost, admin, operator, body, xerr.DatabaseInsertError)
	f.request(http.MethodPost, admin, operator, body, xerr.SingleSubscribeModeExceedsLimit)
	issued := list(member, session)
	require.Len(t, issued, 1)
	sub := issued[0]
	require.Equal(t, starter.ID, sub.SubscribeId)
	require.EqualValues(t, 1024, sub.Traffic)
	require.Equal(t, expires, sub.ExpireTime)
	require.NotEmpty(t, sub.Token)
	id := fmt.Sprintf(`{"user_subscribe_id":"%d"}`, sub.Id)
	retryChange(http.MethodPost, admin+"/toggle", operator, id, xerr.DatabaseUpdateError)
	require.Empty(t, list(member, session))
	paused := list(operatorView, operator)
	require.Len(t, paused, 1)
	require.EqualValues(t, 5, paused[0].Status)
	f.request(http.MethodPost, admin+"/toggle", operator, id, http.StatusOK)
	resumed := list(member, session)
	require.Len(t, resumed, 1)
	require.Equal(t, sub.Token, resumed[0].Token)
	retryChange(http.MethodPut, member+"_token", session, id, xerr.DatabaseUpdateError)
	rotated := list(member, session)
	require.Len(t, rotated, 1)
	require.NotEmpty(t, rotated[0].Token)
	require.NotEqual(t, sub.Token, rotated[0].Token)
	edit := func(end int64) {
		t.Helper()
		payload := fmt.Sprintf(`{"user_subscribe_id":"%d","subscribe_id":"%d","expired_at":%d,"traffic":8192,"upload":123,"download":345}`, sub.Id, standard.ID, end)
		retryChange(http.MethodPut, admin, operator, payload, xerr.DatabaseUpdateError)
	}
	edit(time.Now().Add(-time.Hour).UnixMilli())
	f.request(http.MethodPost, admin+"/toggle", operator, id, xerr.ERROR)
	past := list(operatorView, operator)
	require.Len(t, past, 1)
	require.EqualValues(t, 3, past[0].Status)
	edit(expires)
	revised := list(member, session)
	require.Len(t, revised, 1)
	require.Equal(t, rotated[0].Token, revised[0].Token)
	require.Equal(t, "Standard", revised[0].Subscribe.Name)
	require.EqualValues(t, 1, revised[0].Status)
	require.Equal(t, [3]int64{8192, 123, 345}, [3]int64{revised[0].Traffic, revised[0].Upload, revised[0].Download})
	retryChange(http.MethodPost, admin+"/reset/traffic", operator, id, xerr.DatabaseUpdateError)
	reset := list(member, session)
	require.Len(t, reset, 1)
	require.Equal(t, [3]int64{8192, 0, 0}, [3]int64{reset[0].Traffic, reset[0].Upload, reset[0].Download})
	require.Equal(t, rotated[0].Token, reset[0].Token)
	retryChange(http.MethodDelete, admin, operator, id, xerr.DatabaseDeletedError)
	require.Empty(t, list(member, session))
	require.Empty(t, list(operatorView, operator))
}

func TestCustomerSubscriptionNoteRouteChecksOwnership(t *testing.T) {
	ctx := t.Context()
	database, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	cache := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cache.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, cache), Redis: cache}
	service.Config.JwtAuth.AccessSecret = "customer-route-test-secret"
	owner := client.User.Create().SetPassword("fixture").SaveX(ctx)
	other := client.User.Create().SetPassword("fixture").SaveX(ctx)
	plan := client.Subscribe.Create().SetName("Standard").SaveX(ctx)
	subscription := client.UserSubscribe.Create().SetUserID(owner.ID).SetOrderID(1).SetSubscribeID(plan.ID).
		SetStatus(1).SetToken("owner-subscription").SetExpireTime(time.Now().Add(time.Hour)).SetNote("Existing note").SaveX(ctx)
	issue := func(userID int64) string {
		t.Helper()
		token, err := usersession.Issue(ctx, cache, service.Config.JwtAuth.AccessSecret, 300, userID, "", "email", "", "")
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	ownerToken, otherToken := issue(owner.ID), issue(other.ID)
	router := hertzx.New()
	RegisterHandlers(router, service)
	request := func(method, path, token, payload string, want uint32) json.RawMessage {
		t.Helper()
		req := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(payload))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		var response struct {
			Code uint32          `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.Code != want {
			t.Fatalf("%s %s returned HTTP %d with %s, want code %d (%v)", method, path, recorder.Code, recorder.Body.String(), want, err)
		}
		return response.Data
	}
	const notePath = "/v1/public/user/subscribe_note"
	payload := fmt.Sprintf(`{"user_subscribe_id":"%d","note":"Renamed subscription"}`, subscription.ID)
	request(http.MethodPut, notePath, otherToken, payload, xerr.InvalidAccess)
	request(http.MethodPut, notePath, ownerToken, `{"note":"No subscription ID"}`, xerr.InvalidParams)
	if updated := client.UserSubscribe.GetX(ctx, subscription.ID); updated.Note != subscription.Note {
		t.Fatalf("rejected requests changed the note to %q", updated.Note)
	}
	request(http.MethodPut, notePath, ownerToken, payload, http.StatusOK)
	if updated := client.UserSubscribe.GetX(ctx, subscription.ID); updated.Note != "Renamed subscription" {
		t.Fatalf("authorized request did not persist the note: %q", updated.Note)
	}
	var credentials types.GetOAuthMethodsResponse
	client.UserAuthMethod.Create().SetUserID(owner.ID).SetAuthType("email").SetAuthIdentifier("owner@example.test").SaveX(ctx)
	client.UserAuthMethod.Create().SetUserID(other.ID).SetAuthType("email").SetAuthIdentifier("other@example.test").SaveX(ctx)
	data := request(http.MethodGet, "/v1/public/user/oauth_methods", ownerToken, "", http.StatusOK)
	if err := json.Unmarshal(data, &credentials); err != nil || len(credentials.Methods) != 1 || credentials.Methods[0].AuthIdentifier != "owner@example.test" {
		t.Fatalf("current user's OAuth methods = %s (%v)", data, err)
	}
}
