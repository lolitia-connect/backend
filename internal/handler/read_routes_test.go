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

func TestCampaignPublicationSurvivesFailedEditsAndWithdrawal(t *testing.T) {
	f := newRouteFixture(t)
	_, admin := f.account(true)
	const editor = "/v1/admin/ads/"
	const public = "/v1/common/ads"
	var writesPaused bool
	f.client.Ads.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if writesPaused {
				return nil, errors.New("campaign storage is read-only")
			}
			return next.Mutate(ctx, mutation)
		})
	})
	published := func() []types.Ads {
		t.Helper()
		var result types.GetAdsResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, public, "", "", http.StatusOK), &result))
		return result.List
	}
	write := func(method, body string, failure uint32) {
		t.Helper()
		writesPaused = true
		require.Empty(t, f.request(method, editor, admin, body, failure))
		writesPaused = false
	}
	const initial = `{"title":"Travel offer","type":"text","content":"Monthly pass","target_url":"https://example.test/monthly","status":1}`
	write(http.MethodPost, initial, xerr.DatabaseInsertError)
	require.Empty(t, published())
	f.request(http.MethodPost, editor, admin, initial, http.StatusOK)
	items := published()
	require.Len(t, items, 1)
	original := items[0]
	require.Equal(t, "Monthly pass", original.Content)
	require.Equal(t, "https://example.test/monthly", original.TargetURL)
	const changedTitle = "Annual travel offer"
	updated := fmt.Sprintf(`{"id":"%d","title":%q,"type":"text","content":"Annual pass","target_url":"https://example.test/annual","status":1}`, original.Id, changedTitle)
	write(http.MethodPut, updated, xerr.DatabaseUpdateError)
	require.Equal(t, items, published())
	f.request(http.MethodPut, editor, admin, updated, http.StatusOK)
	var filtered types.GetAdsListResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, editor+"list?search=Annual&status=1&page=1&size=5", admin, "", http.StatusOK), &filtered))
	require.EqualValues(t, 1, filtered.Total)
	require.Len(t, filtered.List, 1)
	require.Equal(t, changedTitle, filtered.List[0].Title)
	require.Equal(t, "https://example.test/annual", filtered.List[0].TargetURL)
	require.Equal(t, filtered.List, published())
	remove := fmt.Sprintf(`{"id":"%d"}`, original.Id)
	write(http.MethodDelete, remove, xerr.DatabaseDeletedError)
	var detail types.Ads
	detailPath := fmt.Sprintf("%sdetail?id=%d", editor, original.Id)
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, detailPath, admin, "", http.StatusOK), &detail))
	require.Equal(t, filtered.List[0], detail)
	require.Equal(t, filtered.List, published())
	f.request(http.MethodDelete, editor, admin, remove, http.StatusOK)
	require.Empty(t, published())
	f.request(http.MethodGet, detailPath, admin, "", xerr.DatabaseQueryError)
}

func TestBatchDocumentRemovalRetriesAsOnePublicationChange(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, admin := f.account(true)
	_, customer := f.account(false)
	var ids []int64
	for _, title := range []string{"Setup", "Billing", "Support"} {
		doc := f.client.Document.Create().SetTitle(title).SetContent(title+" guide").SetShow(true).SaveX(ctx)
		ids = append(ids, doc.ID)
	}
	blocked := true
	f.client.Document.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			id, ok := m.(*ent.DocumentMutation).ID()
			if blocked && ok && id == ids[1] && m.Op().Is(ent.OpDeleteOne) {
				return nil, errors.New("document store unavailable")
			}
			return next.Mutate(ctx, m)
		})
	})
	catalog := func() []string {
		t.Helper()
		var result types.QueryDocumentListResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/document/list", customer, "", http.StatusOK), &result))
		var titles []string
		for _, doc := range result.List {
			titles = append(titles, doc.Title)
		}
		return titles
	}
	path := "/v1/admin/document/batch"
	body := fmt.Sprintf(`{"ids":["%d","%d"]}`, ids[0], ids[1])
	f.request(http.MethodDelete, path, admin, body, xerr.DatabaseDeletedError)
	require.ElementsMatch(t, []string{"Setup", "Billing", "Support"}, catalog())
	blocked = false
	f.request(http.MethodDelete, path, admin, body, http.StatusOK)
	f.request(http.MethodDelete, path, admin, body, http.StatusOK)
	require.Equal(t, []string{"Support"}, catalog())
}

func TestCustomerDocumentCatalogFollowsOperatorPublication(t *testing.T) {
	f := newRouteFixture(t)
	_, admin := f.account(true)
	_, customer := f.account(false)
	const editor = "/v1/admin/document"
	const library = "/v1/public/document"
	read := func(path, token string, result any) {
		t.Helper()
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path, token, "", http.StatusOK), result))
	}
	const draft = `{"title":"Travel guide","content":"Check for updates.","tags":["travel"],"show":false}`
	f.request(http.MethodPost, editor+"/", customer, draft, xerr.InvalidAccess)
	require.Zero(t, f.client.Document.Query().CountX(t.Context()))
	f.request(http.MethodPost, editor+"/", admin, draft, http.StatusOK)
	var drafts types.GetDocumentListResponse
	read(editor+"/list?page=1&size=10&tag=travel", admin, &drafts)
	require.Len(t, drafts.List, 1)
	doc := drafts.List[0]
	require.False(t, doc.Show)
	require.Equal(t, "Check for updates.", doc.Content)
	var hidden types.QueryDocumentListResponse
	read(library+"/list", customer, &hidden)
	require.Empty(t, hidden.List)

	published := fmt.Sprintf(`{"id":"%d","title":"Travel guide","content":"Welcome. {{#if_not_subscribed}}Choose a plan.{{/if_not_subscribed}}","tags":["travel"],"show":true}`, doc.Id)
	f.request(http.MethodPut, editor+"/", admin, published, http.StatusOK)
	var catalog types.QueryDocumentListResponse
	read(library+"/list", customer, &catalog)
	require.EqualValues(t, 1, catalog.Total)
	require.Len(t, catalog.List, 1)
	require.Equal(t, doc.Id, catalog.List[0].Id)
	require.Empty(t, catalog.List[0].Content)
	detail := fmt.Sprintf("%s/detail?id=%d", library, doc.Id)
	var article types.Document
	read(detail, customer, &article)
	require.Equal(t, "Welcome. Choose a plan.", article.Content)
	f.request(http.MethodDelete, editor+"/", admin, fmt.Sprintf(`{"id":"%d"}`, doc.Id), http.StatusOK)
	var removed types.QueryDocumentListResponse
	read(library+"/list", customer, &removed)
	require.Zero(t, removed.Total)
	require.Empty(t, removed.List)
	require.Empty(t, f.request(http.MethodGet, detail, customer, "", xerr.DatabaseQueryError))
}
