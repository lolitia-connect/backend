package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestRegisteredAnnouncementRoutesPublishAndRemoveAnAnnouncement(t *testing.T) {
	ctx := t.Context()
	fixture := newRouteFixture(t)
	_, admin := fixture.account(true)
	_, customer := fixture.account(false)
	client, request := fixture.client, fixture.request
	const adminPath = "/v1/admin/announcement/"
	const publicPath = "/v1/public/announcement/list?page=1&size=10"
	request(http.MethodPost, adminPath, "", `{"title":"Unreachable","content":"Missing session"}`, xerr.ErrorTokenEmpty)
	request(http.MethodPost, adminPath, admin, `{"title":"Maintenance","content":"Service is restarting"}`, http.StatusOK)
	id := client.Announcement.Query().OnlyX(ctx).ID
	detailPath := fmt.Sprintf("%sdetail?id=%d", adminPath, id)
	var draft types.Announcement
	require.NoError(t, json.Unmarshal(request(http.MethodGet, detailPath, admin, "", http.StatusOK), &draft))
	require.Equal(t, "Service is restarting", draft.Content)
	require.NotNil(t, draft.Show)
	require.False(t, *draft.Show)
	visible := func(wantTotal int64) types.QueryAnnouncementResponse {
		t.Helper()
		var list types.QueryAnnouncementResponse
		data := request(http.MethodGet, publicPath, customer, "", http.StatusOK)
		if err := json.Unmarshal(data, &list); err != nil || list.Total != wantTotal || int64(len(list.List)) != wantTotal {
			t.Fatalf("announcement list = %+v, %v (want %d)", list, err, wantTotal)
		}
		return list
	}
	visible(0)
	edited := fmt.Sprintf(`{"id":"%d","title":"Maintenance complete","show":true,"pinned":true}`, id)
	request(http.MethodPut, adminPath, admin, edited, http.StatusOK)
	if item := visible(1).List[0]; item.Id != id || item.Title != "Maintenance complete" || item.Pinned == nil || !*item.Pinned {
		t.Fatalf("published announcement = %+v", item)
	}
	request(http.MethodDelete, adminPath, admin, fmt.Sprintf(`{"id":"%d"}`, id), http.StatusOK)
	visible(0)
	require.Empty(t, request(http.MethodGet, detailPath, admin, "", xerr.DatabaseQueryError))
	if count := client.Announcement.Query().CountX(ctx); count != 0 {
		t.Fatalf("deleted announcement is still stored, count = %d", count)
	}
}
