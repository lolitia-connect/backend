package edge

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	_ "modernc.org/sqlite"
)

func TestManifestReflectsSubscriptionAndAvailableNodes(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, nil)}
	service.Config.Subscribe.ProfileWebPageURL = " https://panel.example.test "
	service.Config.Subscribe.ProfileUpdateInterval = 12

	account := client.User.Create().SetPassword("fixture").SetEnable(true).SaveX(ctx)
	server := client.Server.Create().SetName("Regional").SetCountry("JP").SetCity("Tokyo").
		SetProtocols(`[{"id":"ss","type":"shadowsocks","enable":true,"cipher":"aes-128-gcm"},{"id":"trojan","type":"trojan","enable":true,"security":"tls","transport":"grpc","service_name":"proxy","sni":"tls.example.test"},{"id":"unsupported","type":"vless","enable":true,"security":"reality"}]`).SaveX(ctx)
	addNode := func(name, protocol, protocolID, host string, sort int, enabled bool) *ent.Node {
		t.Helper()
		return client.Node.Create().SetName(name).SetServerID(server.ID).SetProtocol(protocol).
			SetProtocolID(protocolID).SetAddress(host).SetPort(443).SetSort(sort).
			SetEnabled(enabled).SetTags("premium, jp, premium").SaveX(ctx)
	}
	first := addNode("Tokyo", "shadowsocks", "ss", "ss.example.test", 1, true)
	second := addNode("Tokyo", "trojan", "trojan", "trojan.example.test", 1, true)
	unsupported := addNode("Unsupported", "vless", "unsupported", "vless.example.test", 2, true)
	disabled := addNode("Disabled", "trojan", "trojan", "disabled.example.test", 0, false)
	_ = addNode("Unrelated", "trojan", "trojan", "unrelated.example.test", 0, true)
	plan := client.Subscribe.Create().SetName("Regional").
		SetNodes(fmt.Sprintf("%d,%d,%d,%d", first.ID, second.ID, unsupported.ID, disabled.ID)).SaveX(ctx)
	const token = "regional-test-token"
	const credential = "b195dd73-f8fa-46ff-8209-cdc481a72f79"
	now := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	expires := now.Add(24 * time.Hour)
	subscription := client.UserSubscribe.Create().SetUserID(account.ID).SetOrderID(1).SetSubscribeID(plan.ID).
		SetExpireTime(expires).SetTraffic(1000).SetUpload(50).SetDownload(100).SetStatus(1).
		SetToken(token).SetUUID(credential).SaveX(ctx)
	logic := NewManifestLogic(ctx, service)
	logic.now = func() time.Time { return now }

	manifest, err := logic.Manifest(token)
	if err != nil {
		t.Fatal(err)
	}
	if got := manifest.Subscription; got.Name != "Regional" || got.State != "active" || got.TrafficLimit != 1000 ||
		got.Upload != 50 || got.Download != 100 || got.ExpiresAt != expires.Format(time.RFC3339) ||
		got.WebPageURL != "https://panel.example.test" || got.UpdateIntervalHours != 12 {
		t.Fatalf("subscription = %+v", got)
	}
	if len(manifest.Proxies) != 2 {
		t.Fatalf("got %d proxies, want two enabled and supported plan nodes", len(manifest.Proxies))
	}
	if got := manifest.Proxies[0]; got.Name != "Tokyo" || got.Protocol != "shadowsocks" || got.Cipher != "aes-128-gcm" ||
		got.Server != "ss.example.test" || got.Password != credential || !slices.Equal(got.Tags, []string{"premium", "jp"}) {
		t.Fatalf("Shadowsocks proxy = %+v", got)
	}
	if got := manifest.Proxies[1]; got.Name != fmt.Sprintf("Tokyo #%d", second.ID) || got.Protocol != "trojan" ||
		got.Server != "trojan.example.test" || got.Password != credential || got.TLS == nil || got.TLS.ServerName != "tls.example.test" ||
		got.Transport == nil || got.Transport.Type != "grpc" || got.Transport.ServiceName != "proxy" {
		t.Fatalf("Trojan proxy = %+v", got)
	}
	if len(manifest.Notices) != 1 || !strings.Contains(manifest.Notices[0], "Node Unsupported") || !strings.Contains(manifest.Notices[0], "REALITY") {
		t.Fatalf("proxy notices = %v", manifest.Notices)
	}

	now = now.Add(time.Minute)
	refetched, err := logic.Manifest(token)
	if err != nil || manifest.Revision == "" || refetched.Revision != manifest.Revision || refetched.GeneratedAt == manifest.GeneratedAt {
		t.Fatalf("unchanged subscription produced an unstable manifest revision: %v", err)
	}
	client.UserSubscribe.UpdateOneID(subscription.ID).SetDownload(950).SaveX(ctx)
	exhausted, err := logic.Manifest(token)
	if err != nil || exhausted.Subscription.State != "traffic_exhausted" || exhausted.Revision == manifest.Revision ||
		len(exhausted.Proxies) != 0 || !slices.Equal(exhausted.Notices, []string{"Subscription traffic exhausted"}) {
		t.Fatalf("exhausted subscription exposed available proxies or reused its revision: %v", err)
	}
	client.User.UpdateOneID(account.ID).SetEnable(false).SaveX(ctx)
	for _, invalid := range []string{"unknown-token", token} {
		if got, err := logic.Manifest(invalid); got != nil || !errors.Is(err, ErrManifestNotFound) {
			t.Fatalf("missing token or disabled account should be indistinguishable: %v", err)
		}
	}
}

func TestProxyFromProtocol(t *testing.T) {
	item := &node.Node{Id: 7, Name: "Tokyo", Address: "jp.example.com", Port: 443, Protocol: "vless", Tags: "asia, premium", Sort: 3}
	protocol := node.Protocol{
		Type:      "vless",
		Enable:    true,
		Security:  "tls",
		SNI:       "jp.example.com",
		Transport: "ws",
		Host:      "cdn.example.com",
		Path:      "/ws",
		Flow:      "xtls-rprx-vision",
	}

	proxy, supported, reason := proxyFromProtocol(item, protocol, "00000000-0000-4000-8000-000000000001")
	if !supported || reason != "" {
		t.Fatalf("expected proxy to be supported, got supported=%v reason=%q", supported, reason)
	}
	if proxy.UUID == "" || proxy.TLS == nil || proxy.Transport == nil {
		t.Fatalf("expected credentials, tls and transport, got %#v", proxy)
	}
	if proxy.Transport.Type != "ws" || proxy.Transport.Host != "cdn.example.com" {
		t.Fatalf("unexpected transport: %#v", proxy.Transport)
	}
}

func TestProxyFromProtocolRejectsUnsupportedWorkerFeatures(t *testing.T) {
	item := &node.Node{Name: "Reality", Address: "example.com", Port: 443}
	_, supported, reason := proxyFromProtocol(item, node.Protocol{Type: "vless", Enable: true, Security: "reality"}, "user-secret")
	if supported || reason == "" {
		t.Fatalf("expected reality node to be rejected, got supported=%v reason=%q", supported, reason)
	}

	_, supported, reason = proxyFromProtocol(item, node.Protocol{Type: "shadowsocks", Enable: true, Cipher: "2022-blake3-aes-128-gcm"}, "user-secret")
	if supported || reason == "" {
		t.Fatalf("expected shadowsocks 2022 node to be rejected, got supported=%v reason=%q", supported, reason)
	}

	_, supported, reason = proxyFromProtocol(item, node.Protocol{Type: "shadowsocks", Enable: true, Cipher: "aes-128-gcm", Security: "tls"}, "user-secret")
	if supported || reason == "" {
		t.Fatalf("expected Shadowsocks TLS node to be rejected, got supported=%v reason=%q", supported, reason)
	}
}

func TestSubscriptionState(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	if state := subscriptionState(&user.Subscribe{Status: 1, Traffic: 100, Upload: 40, Download: 60}, now); state != "traffic_exhausted" {
		t.Fatalf("expected traffic exhaustion, got %q", state)
	}
	if state := subscriptionState(&user.Subscribe{Status: 5}, now); state != "suspended" {
		t.Fatalf("expected suspended, got %q", state)
	}
	if state := subscriptionState(&user.Subscribe{Status: 255}, now); state != "disabled" {
		t.Fatalf("expected unknown status to be disabled, got %q", state)
	}
	if state := subscriptionState(&user.Subscribe{Status: 1, Traffic: 100, TrafficUnlimited: true, Upload: 40, Download: 60}, now); state != "active" {
		t.Fatalf("expected unlimited traffic to stay active, got %q", state)
	}
}
