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
	server := f.client.Server.Create().SetName("West").SetProtocols(`[{"id":"main","type":"shadowsocks","enable":true,"cipher":"aes-128-gcm"}]`).SaveX(ctx)
	node := f.client.Node.Create().SetName("West relay").SetServerID(server.ID).SetProtocolID("main").SetProtocol("shadowsocks").SetAddress("west.example.test").SetPort(443).SetEnabled(true).SaveX(ctx)
	plan := f.client.Subscribe.Create().SetName("Regional pass").SetNodes(fmt.Sprint(node.ID)).SaveX(ctx)
	const token = "manifest-traffic-fixture"
	sub := f.client.UserSubscribe.Create().SetUserID(member.ID).SetOrderID(1).SetSubscribeID(plan.ID).SetToken(token).
		SetUUID("a6a03360-dae2-48da-8380-77793054ba39").SetExpireTime(time.Now().Add(24 * time.Hour)).SetStatus(1).SetTraffic(1000).SetUpload(30).SetDownload(170).SaveX(ctx)
	fetch := func(id string) types.EdgeManifestResponse {
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
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
		var manifest types.EdgeManifestResponse
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &manifest))
		return manifest
	}
	active := fetch("d72817e2-b81b-4379-8eeb-9bfa490e97d3")
	require.Equal(t, plan.Name, active.Subscription.Name)
	require.Equal(t, "active", active.Subscription.State)
	require.EqualValues(t, 170, active.Subscription.Download)
	require.Len(t, active.Proxies, 1)
	require.Equal(t, node.Address, active.Proxies[0].Server)
	require.Equal(t, sub.UUID, active.Proxies[0].Password)
	f.client.UserSubscribe.UpdateOneID(sub.ID).SetDownload(970).ExecX(ctx)
	exhausted := fetch("2c50fa94-bf7c-4bfa-b30a-c889451eaba7")
	require.Equal(t, "traffic_exhausted", exhausted.Subscription.State)
	require.EqualValues(t, 970, exhausted.Subscription.Download)
	require.Empty(t, exhausted.Proxies)
	require.NotEqual(t, active.Revision, exhausted.Revision)
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
