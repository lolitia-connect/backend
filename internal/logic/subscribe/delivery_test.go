package subscribe

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

const (
	deliveryCredential   = "b195dd73-f8fa-46ff-8209-cdc481a72f79"
	deliveryLinkTemplate = `{{range .Proxies}}{{.Type}}://{{.Password}}@{{.Server}}:{{.Port}}#{{.Name}}{{"\n"}}{{end}}`
)

type deliveryFixture struct {
	service *svc.ServiceContext
	sub     *ent.UserSubscribe
	server  *ent.Server
}

func newDeliveryFixture(t *testing.T) deliveryFixture {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	service := &svc.ServiceContext{Ent: client, Store: repository.NewStore(client, nil)}
	service.Config.Site.SiteName = "Delivery Site"

	ctx := t.Context()
	client.System.Create().SetCategory("group").SetKey("enabled").SetValue("true").SetDesc("").SaveX(ctx)
	account := client.User.Create().SetPassword("fixture").SetEnable(true).SaveX(ctx)
	plan := client.Subscribe.Create().SetName("Regional").SaveX(ctx)
	sub := client.UserSubscribe.Create().SetUserID(account.ID).SetOrderID(1).SetSubscribeID(plan.ID).
		SetExpireTime(time.Now().Add(24 * time.Hour).Truncate(time.Second)).
		SetStatus(1).SetTraffic(1000).SetUpload(100).SetDownload(200).
		SetToken("fixture-token").SetUUID(deliveryCredential).SaveX(ctx)
	server := client.Server.Create().SetName("Fixture server").
		SetProtocols(`[{"id":"edge","type":"vless","enable":true},{"id":"edge","type":"trojan","enable":true}]`).SaveX(ctx)
	client.SubscribeApplication.Create().SetName("Generic").SetUserAgent("generic-client").SetIsDefault(true).
		SetOutputFormat("base64").SetSubscribeTemplate(deliveryLinkTemplate).SetDownloadLink("").SaveX(ctx)
	return deliveryFixture{service: service, sub: sub, server: server}
}

func (f deliveryFixture) node(t *testing.T, name, protocol string, groups ...int64) *ent.NodeCreate {
	t.Helper()
	return f.service.Ent.Node.Create().SetServerID(f.server.ID).SetName(name).
		SetProtocol(protocol).SetProtocolID("edge").SetAddress(name + ".example.test").SetPort(443).
		SetEnabled(true).SetIsHidden(false).SetNodeGroupIds(groups)
}

func TestSubscribeDeliveryNegotiatesFormatAndFiltersNodeVisibility(t *testing.T) {
	f := newDeliveryFixture(t)
	db, ctx := f.service.Ent, t.Context()
	primary := db.NodeGroup.Create().SetName("Plan primary").SetType(group.NodeGroupTypeCommon).SaveX(ctx)
	override := db.NodeGroup.Create().SetName("User override").SetType(group.NodeGroupTypeSubscribe).SaveX(ctx)
	backup := db.NodeGroup.Create().SetName("Backup").SetType(group.NodeGroupTypeCommon).SaveX(ctx)
	appOnly := db.NodeGroup.Create().SetName("App only").SetType(group.NodeGroupTypeApp).SaveX(ctx)
	db.Subscribe.UpdateOneID(f.sub.SubscribeID).SetNodeGroupID(primary.ID).
		SetNodeGroupIds([]int64{backup.ID, appOnly.ID}).SaveX(ctx)
	db.UserSubscribe.UpdateOneID(f.sub.ID).SetNodeGroupID(override.ID).SaveX(ctx)

	f.node(t, "override", "vless", override.ID, backup.ID).SaveX(ctx)
	f.node(t, "backup", "vless", backup.ID).SaveX(ctx)
	f.node(t, "public", "trojan").SetNodeGroupIds([]int64{}).SaveX(ctx)
	f.node(t, "replaced-primary", "vless", primary.ID).SaveX(ctx)
	f.node(t, "app-only", "vless", appOnly.ID).SaveX(ctx)
	f.node(t, "hidden-grouped", "vless", override.ID).SetIsHidden(true).SaveX(ctx)
	f.node(t, "hidden-public", "vless").SetIsHidden(true).SaveX(ctx)
	f.node(t, "disabled-grouped", "vless", backup.ID).SetEnabled(false).SaveX(ctx)
	f.node(t, "disabled-public", "vless").SetEnabled(false).SaveX(ctx)

	db.SubscribeApplication.Create().SetName("JSON client").SetUserAgent("json-client").SetOutputFormat("JSON").
		SetDefaultParams("mode=rule&emoji=1").SetDownloadLink("").
		SetSubscribeTemplate(`{{dict "plan" .SubscribeName "mode" .Params.mode "emoji" .Params.emoji "proxies" .Proxies | toJson}}`).SaveX(ctx)

	type proxy struct {
		Name, Server, Type, Password string
		Port                         uint16
	}
	overrideProxy := proxy{Name: "override", Server: "override.example.test", Type: "vless", Password: deliveryCredential, Port: 443}
	backupProxy := proxy{Name: "backup", Server: "backup.example.test", Type: "vless", Password: deliveryCredential, Port: 443}
	publicProxy := proxy{Name: "public", Server: "public.example.test", Type: "trojan", Password: deliveryCredential, Port: 443}

	for _, tc := range []struct {
		name, userAgent, agent, protocol, mode string
		params                                 map[string]string
		wantJSON                               []proxy
	}{
		{
			name: "explicit agent overrides user agent and applies request params", userAgent: "generic-client/1.0",
			agent: " JSON-CLIENT ", protocol: "VLESS", params: map[string]string{"mode": "global"},
			mode: "global", wantJSON: []proxy{overrideProxy, backupProxy},
		},
		{
			name: "unknown explicit agent falls back to user agent", userAgent: "Example JSON-CLIENT/1.0",
			agent: "unknown", mode: "rule", wantJSON: []proxy{overrideProxy, backupProxy, publicProxy},
		},
		{name: "unmatched user agent gets the default base64 list", userAgent: "unknown client"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logic := NewSubscribeLogic(t.Context(), f.service, RequestMeta{UserAgent: tc.userAgent})
			resp, err := logic.Handler(&types.SubscribeRequest{
				Token: f.sub.Token, UA: tc.userAgent, Agent: tc.agent, Type: tc.protocol, Params: tc.params,
			})
			require.NoError(t, err)
			require.Equal(t, fmt.Sprintf("upload=100;download=200;total=1000;expire=%d", f.sub.ExpireTime.Unix()), resp.Header)

			if tc.wantJSON != nil {
				require.Equal(t, "attachment;filename*=UTF-8''Delivery+Site.json", resp.Headers["Content-Disposition"])
				require.Equal(t, "application/octet-stream; charset=UTF-8", resp.Headers["Content-Type"])
				var config struct {
					Plan, Mode, Emoji string
					Proxies           []proxy
				}
				require.NoError(t, json.Unmarshal(resp.Config, &config))
				require.Equal(t, "Regional", config.Plan)
				require.Equal(t, tc.mode, config.Mode)
				require.Equal(t, "1", config.Emoji)
				require.ElementsMatch(t, tc.wantJSON, config.Proxies)
				return
			}

			require.Empty(t, resp.Headers)
			decoded, err := base64.StdEncoding.DecodeString(string(resp.Config))
			require.NoError(t, err)
			require.ElementsMatch(t, []string{
				"vless://" + deliveryCredential + "@override.example.test:443#override",
				"vless://" + deliveryCredential + "@backup.example.test:443#backup",
				"trojan://" + deliveryCredential + "@public.example.test:443#public",
			}, strings.Split(strings.TrimSuffix(string(decoded), "\n"), "\n"))
		})
	}
}

func TestSubscribeDeliveryEnforcesExpiredGroupLimits(t *testing.T) {
	f := newDeliveryFixture(t)
	db, ctx := f.service.Ent, t.Context()
	primary := db.NodeGroup.Create().SetName("Active subscribers").SetType(group.NodeGroupTypeCommon).SaveX(ctx)
	fallback := db.NodeGroup.Create().SetName("Expired subscribers").SetType(group.NodeGroupTypeSubscribe).
		SetIsExpiredGroup(true).SetExpiredDaysLimit(2).SetMaxTrafficGBExpired(1).SaveX(ctx)
	db.Subscribe.UpdateOneID(f.sub.SubscribeID).SetNodeGroupID(primary.ID).SaveX(ctx)
	f.node(t, "active-only", "vless", primary.ID).SaveX(ctx)
	f.node(t, "public", "vless").SaveX(ctx)
	f.node(t, "expired-visible", "vless", fallback.ID).SaveX(ctx)
	f.node(t, "expired-hidden", "vless", fallback.ID).SetIsHidden(true).SaveX(ctx)

	const gib = 1 << 30
	for _, tc := range []struct {
		name            string
		expiredFor      time.Duration
		expiredDownload int64
		wantCode        uint32
	}{
		{name: "within grace period and one byte under the cap", expiredFor: 36 * time.Hour, expiredDownload: gib/2 - 1},
		{name: "at the expired traffic cap", expiredFor: 36 * time.Hour, expiredDownload: gib / 2, wantCode: xerr.SubscribeExpired},
		{name: "past the grace period", expiredFor: 84 * time.Hour, expiredDownload: 0, wantCode: xerr.SubscribeExpired},
		{name: "active subscription at its regular traffic cap", expiredFor: -24 * time.Hour, wantCode: xerr.TrafficExhausted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			db.UserSubscribe.UpdateOneID(f.sub.ID).SetExpireTime(time.Now().Add(-tc.expiredFor)).
				SetTraffic(3*gib + 200).SetUpload(3 * gib).SetExpiredUpload(gib / 2).SetExpiredDownload(tc.expiredDownload).SaveX(ctx)

			resp, err := NewSubscribeLogic(ctx, f.service, RequestMeta{UserAgent: "generic-client"}).
				Handler(&types.SubscribeRequest{Token: f.sub.Token, UA: "generic-client"})
			if tc.wantCode != 0 {
				var code *xerr.CodeError
				require.ErrorAs(t, err, &code)
				require.Equal(t, tc.wantCode, code.GetErrCode())
				require.Nil(t, resp)
				return
			}

			require.NoError(t, err)
			decoded, err := base64.StdEncoding.DecodeString(string(resp.Config))
			require.NoError(t, err)
			require.Equal(t, "vless://"+deliveryCredential+"@expired-visible.example.test:443#expired-visible\n", string(decoded))
		})
	}
}
