package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestBulkSubscriptionTokenRotationIsAtomicAcrossActiveAndFinishedPlans(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	member, session := f.account(false)
	plan := f.client.Subscribe.Create().SetName("Summer pass").SaveX(ctx)
	create := func(status uint8, token string) *ent.UserSubscribe {
		t.Helper()
		return f.client.UserSubscribe.Create().SetUserID(member.ID).SetOrderID(int64(status)).SetSubscribeID(plan.ID).
			SetStatus(status).SetToken(token).SetUUID(token + "-node").SetTraffic(1000).SetDownload(350).SaveX(ctx)
	}
	active, finished, retired := create(1, "active-link"), create(2, "finished-link"), create(3, "retired-link")
	const path = "/v1/admin/subscribe/reset_all_token"
	f.request(http.MethodPost, path, session, `{}`, xerr.InvalidAccess)
	writes := 0
	f.client.UserSubscribe.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if _, rotating := m.(*ent.UserSubscribeMutation).Token(); rotating {
				writes++
				if writes == 2 {
					return nil, errors.New("credential update interrupted")
				}
			}
			return next.Mutate(ctx, m)
		})
	})
	f.request(http.MethodPost, path, operator, `{}`, xerr.DatabaseUpdateError)
	require.Equal(t, 2, writes)
	lookup := f.service.Store.User().FindOneSubscribeByToken
	for _, original := range []*ent.UserSubscribe{active, finished, retired} {
		found, err := lookup(ctx, original.Token)
		require.NoError(t, err)
		require.Equal(t, original.ID, found.Id)
		require.Equal(t, original.UUID, f.client.UserSubscribe.GetX(ctx, original.ID).UUID)
	}
	var result types.ResetAllSubscribeTokenResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, path, operator, `{}`, http.StatusOK), &result))
	require.True(t, result.Success)
	for _, original := range []*ent.UserSubscribe{active, finished} {
		_, err := lookup(ctx, original.Token)
		require.True(t, ent.IsNotFound(err))
		updated := f.client.UserSubscribe.GetX(ctx, original.ID)
		require.NotEmpty(t, updated.UUID)
		require.NotEqual(t, original.UUID, updated.UUID)
		require.NotEmpty(t, updated.Token)
		found, err := lookup(ctx, updated.Token)
		require.NoError(t, err)
		require.Equal(t, original.ID, found.Id)
		require.Equal(t, original.Download, found.Download)
	}
	found, err := lookup(ctx, retired.Token)
	require.NoError(t, err)
	require.Equal(t, retired.ID, found.Id)
	require.Equal(t, retired.UUID, f.client.UserSubscribe.GetX(ctx, retired.ID).UUID)
}

func TestEdgeManifestReflectsTrafficExhaustionOnTheNextFetch(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	const renderer = "fixture-renderer"
	const secret = "manifest-test-secret"
	f.service.Config.EdgeSubscribe = config.EdgeSubscribeConfig{Enabled: true, Keys: []config.EdgeSubscribeAccessKey{{ID: renderer, Secret: secret}}}
	RegisterNativeHandlers(f.router.Hertz(), f.service)
	member, _ := f.account(false)
	server := f.client.Server.Create().SetName("West").SetProtocols(`[{"id":"legacy","type":"shadowsocks","enable":true,"cipher":"aes-256-gcm"},{"id":"main","type":"shadowsocks","enable":true,"cipher":"aes-128-gcm"}]`).SaveX(ctx)
	node := f.client.Node.Create().SetName("West relay").SetServerID(server.ID).SetProtocolID("main").SetProtocol("shadowsocks").SetAddress("west.example.test").SetPort(443).SetEnabled(true).SaveX(ctx)
	plan := f.client.Subscribe.Create().SetName("Regional pass").SetNodes(fmt.Sprint(node.ID)).SaveX(ctx)
	const token = "manifest-traffic-fixture"
	sub := f.client.UserSubscribe.Create().SetUserID(member.ID).SetOrderID(1).SetSubscribeID(plan.ID).SetToken(token).
		SetUUID("a6a03360-dae2-48da-8380-77793054ba39").SetExpireTime(time.Now().Add(24 * time.Hour)).SetStatus(1).SetTraffic(1000).SetUpload(30).SetDownload(170).SaveX(ctx)
	fetch := func(id string, wantStatus int) types.EdgeManifestResponse {
		t.Helper()
		const path = "/api/edge/v1/manifest"
		stamp := fmt.Sprint(time.Now().Unix())
		tokenHash, idHash := sha256.Sum256([]byte(token)), sha256.Sum256([]byte(id))
		signature := hmac.New(sha256.New, []byte(secret))
		fmt.Fprintf(signature, "v2\nGET\n%s\n%x\n%s\n%x", path, tokenHash, stamp, idHash)
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, path+"?token="+token, nil)
		req.Header.Set("X-Request-ID", id)
		req.Header.Set("Authorization", "PPanel-Edge-HMAC kid="+renderer+", ts="+stamp+", sig="+hex.EncodeToString(signature.Sum(nil)))
		recorder := httptest.NewRecorder()
		f.router.ServeHTTP(recorder, req)
		require.Equal(t, wantStatus, recorder.Code, recorder.Body.String())
		if wantStatus != http.StatusOK {
			require.Equal(t, http.StatusText(wantStatus), recorder.Body.String())
			return types.EdgeManifestResponse{}
		}
		require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
		var manifest types.EdgeManifestResponse
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &manifest))
		return manifest
	}
	var offline string
	outage := func(entity string) ent.Interceptor {
		return ent.InterceptFunc(func(next ent.Querier) ent.Querier {
			return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
				if entity == offline {
					return nil, errors.New("manifest catalog offline")
				}
				return next.Query(ctx, q)
			})
		})
	}
	f.client.UserSubscribe.Intercept(outage(ent.TypeUserSubscribe))
	f.client.User.Intercept(outage(ent.TypeUser))
	f.client.Subscribe.Intercept(outage(ent.TypeSubscribe))
	f.client.Node.Intercept(outage(ent.TypeNode))
	for _, offline = range []string{ent.TypeUserSubscribe, ent.TypeUser, ent.TypeSubscribe, ent.TypeNode} {
		id := uuid.NewString()
		fetch(id, http.StatusInternalServerError)
		fetch(id, http.StatusNotFound)
	}
	offline = ""
	active := fetch(uuid.NewString(), http.StatusOK)
	require.Equal(t, plan.Name, active.Subscription.Name)
	require.Equal(t, "active", active.Subscription.State)
	require.EqualValues(t, 170, active.Subscription.Download)
	require.Len(t, active.Proxies, 1)
	require.Equal(t, node.Address, active.Proxies[0].Server)
	require.Equal(t, sub.UUID, active.Proxies[0].Password)
	require.Equal(t, "aes-128-gcm", active.Proxies[0].Cipher)
	f.client.Node.UpdateOneID(node.ID).SetProtocolID("retired").ExecX(ctx)
	retired := fetch(uuid.NewString(), http.StatusOK)
	require.Empty(t, retired.Proxies)
	require.Len(t, retired.Notices, 1)
	require.Contains(t, retired.Notices[0], node.Name)
	f.client.UserSubscribe.UpdateOneID(sub.ID).SetDownload(970).ExecX(ctx)
	exhausted := fetch(uuid.NewString(), http.StatusOK)
	require.Equal(t, "traffic_exhausted", exhausted.Subscription.State)
	require.EqualValues(t, 970, exhausted.Subscription.Download)
	require.Empty(t, exhausted.Proxies)
	require.NotEqual(t, active.Revision, exhausted.Revision)
	f.client.Node.UpdateOneID(node.ID).SetProtocolID("main").ExecX(ctx)
	for _, state := range []struct {
		status       uint8
		name, notice string
	}{{5, "suspended", "Subscription suspended"}, {2, "expired", "Subscription expired"}, {4, "disabled", "Subscription is unavailable"}} {
		f.client.UserSubscribe.UpdateOneID(sub.ID).SetStatus(state.status).SetDownload(170).ExecX(ctx)
		unavailable := fetch(uuid.NewString(), http.StatusOK)
		require.Equal(t, state.name, unavailable.Subscription.State)
		require.Equal(t, []string{state.notice}, unavailable.Notices)
		require.Empty(t, unavailable.Proxies)
		require.NotEqual(t, active.Revision, unavailable.Revision)
	}
	f.client.UserSubscribe.UpdateOneID(sub.ID).SetStatus(1).ExecX(ctx)
	resumed := fetch(uuid.NewString(), http.StatusOK)
	require.Equal(t, active.Proxies, resumed.Proxies)
	require.Equal(t, "active", resumed.Subscription.State)
	require.Empty(t, resumed.Notices)
}

func TestWildcardSubscriptionDownloadsRecoverWithoutServingPartialProfiles(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	f.service.Ent = f.client
	f.service.Config.Subscribe.PanDomain = true
	f.service.Config.Subscribe.SubscribePath = "/downloads"
	f.service.Config.Subscribe.UserAgentLimit = true
	f.service.Config.Subscribe.UserAgentList = "AtlasReader"
	RegisterNativeHandlers(f.router.Hertz(), f.service)
	owner, session := f.account(false)
	plan := f.client.Subscribe.Create().SetName("Travel pass").SaveX(ctx)
	expires := time.Now().AddDate(0, 0, 3).Truncate(time.Second)
	sub := f.client.UserSubscribe.Create().SetUserID(owner.ID).SetOrderID(1).SetSubscribeID(plan.ID).
		SetToken("wildcard-member").SetStatus(1).SetTraffic(900).SetDownload(150).SetExpireTime(expires).SaveX(ctx)
	f.client.SubscribeApplication.Create().SetName("Atlas Reader").SetUserAgent("AtlasReader").SetDownloadLink("").
		SetOutputFormat("json").SetSubscribeTemplate(`{{dict "plan" .SubscribeName "mode" .Params.mode | toJson}}`).SaveX(ctx)
	var memberships types.QueryUserSubscribeListResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/public/user/subscribe", session, "", http.StatusOK), &memberships))
	require.Len(t, memberships.List, 1)
	issued := memberships.List[0]
	require.Equal(t, sub.ID, issued.Id)
	require.NotEmpty(t, issued.Short)
	request := func(host, path, agent string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
		req.Host = host
		req.Header.Set("User-Agent", agent)
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		require.Equal(t, status, rec.Code, rec.Body.String())
		if status != http.StatusOK {
			require.Empty(t, rec.Header().Get("subscription-userinfo"))
			require.NotContains(t, rec.Body.String(), plan.Name)
		}
		return rec
	}
	host := issued.Short + ".plans.example.test"
	path := "/downloads?mode=travel&token=" + issued.Token
	rootHost := issued.Token + ".plans.example.test"
	const reader = "AtlasReader/2"
	request("retired.plans.example.test", path, reader, http.StatusForbidden)
	request("localhost", "/", reader, http.StatusForbidden)
	request(rootHost, "/", "RetiredReader", http.StatusForbidden)
	var offline string
	interrupt := func(entity string) ent.Interceptor {
		return ent.InterceptFunc(func(next ent.Querier) ent.Querier {
			return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
				if entity == offline {
					return nil, errors.New("download store unavailable")
				}
				return next.Query(ctx, q)
			})
		})
	}
	f.client.SubscribeApplication.Intercept(interrupt(ent.TypeSubscribeApplication))
	f.client.UserSubscribe.Intercept(interrupt(ent.TypeUserSubscribe))
	f.client.User.Intercept(interrupt(ent.TypeUser))
	f.client.Subscribe.Intercept(interrupt(ent.TypeSubscribe))
	for _, offline = range []string{ent.TypeSubscribeApplication, ent.TypeUserSubscribe, ent.TypeUser, ent.TypeSubscribe} {
		request(host, path, reader, http.StatusInternalServerError)
	}
	offline = ""
	for _, link := range []struct{ host, path string }{{host, path}, {rootHost, "/?mode=travel"}} {
		profile := request(link.host, link.path, reader, http.StatusOK)
		require.JSONEq(t, `{"plan":"Travel pass","mode":"travel"}`, profile.Body.String())
		require.Equal(t, fmt.Sprintf("upload=0;download=150;total=900;expire=%d", expires.Unix()), profile.Header().Get("subscription-userinfo"))
	}
	f.client.User.UpdateOneID(owner.ID).SetEnable(false).ExecX(ctx)
	request(rootHost, "/", reader, http.StatusForbidden)
	f.client.UserSubscribe.DeleteOneID(sub.ID).ExecX(ctx)
	request(host, path, reader, http.StatusNotFound)
}

func TestClientCatalogPublishesEditsAndRetirementOnlyAfterStorageRecovers(t *testing.T) {
	f := newRouteFixture(t)
	_, admin := f.account(true)
	const editor = "/v1/admin/application"
	const catalog = editor + "/subscribe_application_list?page=1&size=10"
	const public = "/v1/common/client"
	var readUnavailable, writeUnavailable bool
	f.client.SubscribeApplication.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
			if readUnavailable {
				return nil, errors.New("application catalog unavailable")
			}
			return next.Query(ctx, q)
		})
	}))
	f.client.SubscribeApplication.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if writeUnavailable {
				return nil, errors.New("application catalog is read-only")
			}
			return next.Mutate(ctx, m)
		})
	})
	check := func(id int64, version string) {
		t.Helper()
		var internal types.GetSubscribeApplicationListResponse
		var clients types.GetSubscribeClientResponse
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, catalog, admin, "", http.StatusOK), &internal))
		raw := f.request(http.MethodGet, public, "", "", http.StatusOK)
		require.NoError(t, json.Unmarshal(raw, &clients))
		if version == "" {
			require.Zero(t, internal.Total)
			require.Empty(t, internal.List)
			require.Zero(t, clients.Total)
			require.Empty(t, clients.List)
			return
		}
		require.Len(t, internal.List, 1)
		require.Len(t, clients.List, 1)
		require.Equal(t, id, clients.List[0].Id)
		require.Equal(t, id, internal.List[0].Id)
		require.Equal(t, "profile-"+version, internal.List[0].SubscribeTemplate)
		require.NotContains(t, string(raw), "profile-"+version)
		require.Equal(t, "https://reader.example.test/"+version, clients.List[0].DownloadLink.Linux)
		require.Equal(t, clients.List[0].DownloadLink, internal.List[0].DownloadLink)
	}
	form := func(id int64, version string) string {
		return fmt.Sprintf(`{"id":"%d","name":"Reader","user_agent":"Reader","template":"profile-%s","download_link":{"linux":"https://reader.example.test/%s"}}`, id, version, version)
	}
	writeUnavailable = true
	f.request(http.MethodPost, editor+"/", admin, form(0, "one"), xerr.DatabaseInsertError)
	check(0, "")
	writeUnavailable = false
	var app types.SubscribeApplication
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, editor+"/", admin, form(0, "one"), http.StatusOK), &app))
	check(app.Id, "one")
	const edit = editor + "/subscribe_application"
	readUnavailable = true
	require.Empty(t, f.request(http.MethodGet, catalog, admin, "", xerr.DatabaseQueryError))
	require.Empty(t, f.request(http.MethodGet, public, "", "", xerr.DatabaseQueryError))
	f.request(http.MethodPut, edit, admin, form(app.Id, "two"), xerr.DatabaseQueryError)
	readUnavailable, writeUnavailable = false, true
	f.request(http.MethodPut, edit, admin, form(app.Id, "two"), xerr.DatabaseUpdateError)
	check(app.Id, "one")
	writeUnavailable = false
	f.request(http.MethodPut, edit, admin, form(app.Id, "two"), http.StatusOK)
	check(app.Id, "two")
	remove := fmt.Sprintf(`{"id":"%d"}`, app.Id)
	writeUnavailable = true
	f.request(http.MethodDelete, edit, admin, remove, xerr.DatabaseDeletedError)
	check(app.Id, "two")
	writeUnavailable = false
	f.request(http.MethodDelete, edit, admin, remove, http.StatusOK)
	check(0, "")
}

func TestSubscriberDownloadsPublishedClientTemplate(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	f.service.Ent = f.client
	f.service.Config.Site.SiteName = "Atlas"
	f.service.Config.Subscribe.SubscribePath = "/download"
	f.service.Config.Subscribe.UserAgentLimit = true
	RegisterNativeHandlers(f.router.Hertz(), f.service)
	_, admin := f.account(true)
	member, _ := f.account(false)
	plan := f.client.Subscribe.Create().SetName("Day pass").SaveX(ctx)
	expires := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	sub := f.client.UserSubscribe.Create().SetSubscribeID(plan.ID).SetUserID(member.ID).SetOrderID(1).
		SetStatus(1).SetToken("reader-fixture").SetExpireTime(expires).SetUpload(17).SetDownload(52).SetTraffic(1024).SaveX(ctx)
	app := types.CreateSubscribeApplicationRequest{
		Name: "Reader", UserAgent: "AtlasReader", IsDefault: true, OutputFormat: "base64",
		SubscribeTemplate: `{{.SubscribeName}}/{{.Params.mode}}`, DefaultParams: "mode=rule",
		DownloadLink: types.DownloadLink{Windows: "https://reader.example.test/download"},
	}
	payload, err := json.Marshal(app)
	require.NoError(t, err)
	var published types.SubscribeApplication
	const apps = "/v1/admin/application"
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, apps+"/", admin, string(payload), http.StatusOK), &published))
	var catalog struct {
		List []types.SubscribeClient `json:"list"`
	}
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, "/v1/common/client", "", "", http.StatusOK), &catalog))
	require.Len(t, catalog.List, 1)
	require.Equal(t, published.Id, catalog.List[0].Id)
	require.Equal(t, app.DownloadLink, catalog.List[0].DownloadLink)
	download := func(agent, path, token string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
		req.Header.Set("User-Agent", agent)
		if token != "" {
			req.Header.Set("token", token)
		}
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		require.Equal(t, status, rec.Code, rec.Body.String())
		return rec
	}
	const reader = "AtlasReader/2"
	download("Retired client", "/download", sub.Token, http.StatusForbidden)
	download(reader, "/download?token=unknown", "", http.StatusNotFound)
	first := download(reader, "/download", sub.Token, http.StatusOK)
	require.Equal(t, "text/plain; charset=utf-8", first.Header().Get("Content-Type"))
	content, err := base64.StdEncoding.DecodeString(first.Body.String())
	require.NoError(t, err)
	require.Equal(t, "Day pass/rule", string(content))
	require.Equal(t, fmt.Sprintf("upload=17;download=52;total=1024;expire=%d", expires.Unix()), first.Header().Get("subscription-userinfo"))

	updated := types.UpdateSubscribeApplicationRequest{Id: published.Id, Name: app.Name, UserAgent: app.UserAgent, IsDefault: true,
		OutputFormat: "json", DefaultParams: "mode=auto", SubscribeTemplate: `{{dict "plan" .SubscribeName "mode" .Params.mode | toJson}}`, DownloadLink: app.DownloadLink}
	payload, err = json.Marshal(updated)
	require.NoError(t, err)
	f.request(http.MethodPut, apps+"/subscribe_application", admin, string(payload), http.StatusOK)
	var preview types.PreviewSubscribeTemplateResponse
	previewPath := fmt.Sprintf("%s/preview?id=%d", apps, published.Id)
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, previewPath, admin, "", http.StatusOK), &preview))
	require.JSONEq(t, `{"plan":"Test Subscribe","mode":"auto"}`, preview.Template)
	jsonFile := download(reader, "/download?mode=travel&token="+sub.Token, "", http.StatusOK)
	require.JSONEq(t, `{"plan":"Day pass","mode":"travel"}`, jsonFile.Body.String())
	require.Contains(t, jsonFile.Header().Get("Content-Disposition"), "Atlas.json")
	require.Equal(t, "application/octet-stream; charset=UTF-8", jsonFile.Header().Get("Content-Type"))
	f.client.UserSubscribe.UpdateOneID(sub.ID).SetExpireTime(time.Now().Add(-time.Hour)).ExecX(ctx)
	expired := download(reader, "/download", sub.Token, http.StatusForbidden)
	require.Empty(t, expired.Header().Get("subscription-userinfo"))
}
