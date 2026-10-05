package server

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	serverv1 "github.com/perfect-panel/server/api/server/v1"
	"github.com/perfect-panel/server/internal/types"
	"google.golang.org/protobuf/proto"
)

func newProtobufContext(t *testing.T, message proto.Message) *app.RequestContext {
	t.Helper()
	body, err := proto.Marshal(message)
	if err != nil {
		t.Fatalf("proto.Marshal() error = %v", err)
	}
	ctx := app.NewContext(0)
	ctx.Request.Header.SetContentTypeBytes([]byte("application/protobuf; charset=binary"))
	ctx.Request.SetBody(body)
	return ctx
}

func TestBindOnlineUsersRequest_Protobuf(t *testing.T) {
	ctx := newProtobufContext(t, &serverv1.PushOnlineUsersRequest{
		Users: []*serverv1.OnlineUser{{UserId: 42, Ip: "203.0.113.1"}},
	})
	var request types.OnlineUsersRequest
	if err := bindOnlineUsersRequest(ctx, &request); err != nil {
		t.Fatalf("bindOnlineUsersRequest() error = %v", err)
	}
	if len(request.Users) != 1 || request.Users[0].SID != 42 || request.Users[0].IP != "203.0.113.1" {
		t.Fatalf("users = %+v, want converted protobuf user", request.Users)
	}
}

func TestBindUserTrafficRequest_Protobuf(t *testing.T) {
	ctx := newProtobufContext(t, &serverv1.PushUserTrafficRequest{
		Traffic: []*serverv1.UserTraffic{{UserId: 42, Upload: 100, Download: 200}},
	})
	var request types.ServerPushUserTrafficRequest
	if err := bindUserTrafficRequest(ctx, &request); err != nil {
		t.Fatalf("bindUserTrafficRequest() error = %v", err)
	}
	if len(request.Traffic) != 1 || request.Traffic[0] != (types.UserTraffic{SID: 42, Upload: 100, Download: 200}) {
		t.Fatalf("traffic = %+v, want converted protobuf traffic", request.Traffic)
	}
}

func TestBindServerStatusRequest_Protobuf(t *testing.T) {
	ctx := newProtobufContext(t, &serverv1.PushServerStatusRequest{
		Cpu: 0.5, Mem: 0.6, Disk: 0.7, UpdatedAt: 123,
	})
	var request types.ServerPushStatusRequest
	if err := bindServerStatusRequest(ctx, &request); err != nil {
		t.Fatalf("bindServerStatusRequest() error = %v", err)
	}
	if request.Cpu != 0.5 || request.Mem != 0.6 || request.Disk != 0.7 || request.UpdatedAt != 123 {
		t.Fatalf("request = %+v, want converted protobuf status", request)
	}
}

func TestBindOnlineUsersRequest_JSON(t *testing.T) {
	ctx := app.NewContext(0)
	ctx.Request.Header.SetContentTypeBytes([]byte("application/json"))
	ctx.Request.SetBodyString(`{"users":[{"uid":42,"ip":"203.0.113.1"}]}`)
	var request types.OnlineUsersRequest
	if err := bindOnlineUsersRequest(ctx, &request); err != nil {
		t.Fatalf("bindOnlineUsersRequest() error = %v", err)
	}
	if len(request.Users) != 1 || request.Users[0].SID != 42 || request.Users[0].IP != "203.0.113.1" {
		t.Fatalf("users = %+v, want decoded JSON user", request.Users)
	}
}

func TestBindOnlineUsersRequest_EmptyWireEntryBecomesZeroUser(t *testing.T) {
	// An empty sub-message on the wire is a valid, non-nil entry: protobuf
	// cannot distinguish it from an omitted one, so it decodes as a zero user
	// and is forwarded as such. This matches the JSON API, which accepts
	// {"uid":0,"ip":""} the same way.
	ctx := newProtobufContext(t, &serverv1.PushOnlineUsersRequest{
		Users: []*serverv1.OnlineUser{nil, {UserId: 7, Ip: "198.51.100.1"}},
	})
	var request types.OnlineUsersRequest
	if err := bindOnlineUsersRequest(ctx, &request); err != nil {
		t.Fatalf("bindOnlineUsersRequest() error = %v", err)
	}
	if len(request.Users) != 2 || request.Users[0].SID != 0 || request.Users[1].SID != 7 {
		t.Fatalf("users = %+v, want a zero user followed by the reported one", request.Users)
	}
}

func TestWriteServerReportResult_Protobuf(t *testing.T) {
	ctx := app.NewContext(0)
	ctx.Request.Header.Set("Accept", "application/protobuf, application/json;q=0.9")
	writeServerReportResult(ctx, nil)

	if got := string(ctx.Response.Header.ContentType()); got != protobufContentType {
		t.Fatalf("Content-Type = %q, want %q", got, protobufContentType)
	}
	var result serverv1.Result
	if err := proto.Unmarshal(ctx.Response.Body(), &result); err != nil {
		t.Fatalf("proto.Unmarshal() error = %v", err)
	}
	if result.Code != 200 || result.Message != "success" {
		t.Fatalf("result = {Code:%d Message:%q}, want successful protobuf result", result.Code, result.Message)
	}
}

func TestWriteServerReportResult_DefaultsToJSON(t *testing.T) {
	ctx := app.NewContext(0)
	writeServerReportResult(ctx, nil)

	if got := string(ctx.Response.Header.ContentType()); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want JSON", got)
	}
	var response struct {
		Code uint32 `json:"code"`
	}
	if err := json.Unmarshal(ctx.Response.Body(), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if response.Code != 200 {
		t.Fatalf("response code = %d, want 200", response.Code)
	}
}

func TestWriteServerReportResult_ProtobufError(t *testing.T) {
	ctx := app.NewContext(0)
	ctx.Request.Header.Set("Accept", protobufContentType)
	writeServerReportResult(ctx, errors.New("report failed"))

	if got := string(ctx.Response.Header.ContentType()); got != protobufContentType {
		t.Fatalf("Content-Type = %q, want %q", got, protobufContentType)
	}
	var response serverv1.Result
	if err := proto.Unmarshal(ctx.Response.Body(), &response); err != nil {
		t.Fatalf("proto.Unmarshal() error = %v", err)
	}
	if response.Code != 500 || response.Message != "Internal Server Error" {
		t.Fatalf("response = {Code:%d Message:%q}, want internal-error protobuf envelope", response.Code, response.Message)
	}
}

func TestResponseConversions(t *testing.T) {
	config, err := serverConfigResponseToProtobuf(&types.GetServerConfigResponse{
		Basic:    types.ServerBasic{PushInterval: 60, PullInterval: 30},
		Protocol: "vless",
		Config:   map[string]interface{}{"port": 443},
	})
	if err != nil {
		t.Fatalf("serverConfigResponseToProtobuf() error = %v", err)
	}
	if config.Code != 200 || config.Data.Protocol != "vless" || config.Data.Basic.PushInterval != 60 {
		t.Fatalf("config = %+v, want protobuf config envelope", config)
	}
	if got := config.Data.Config.AsMap()["port"]; got != float64(443) {
		t.Fatalf("config port = %#v, want 443", got)
	}

	users := serverUserListResponseToProtobuf(&types.GetServerUserListResponse{Users: []types.ServerUser{{
		Id: 1, UUID: "uuid", SpeedLimit: 10, DeviceLimit: 2,
	}}})
	if users.Code != 200 || len(users.Data.Users) != 1 || users.Data.Users[0].Uuid != "uuid" {
		t.Fatalf("users = %+v, want protobuf user-list envelope", users)
	}

	protocols, err := queryServerProtocolConfigResponseToProtobuf(&types.QueryServerConfigResponse{
		TrafficReportThreshold: 1024,
		PushInterval:           60,
		PullInterval:           30,
		IPStrategy:             "prefer_ipv4",
		DNS: []types.NodeDNS{{
			Proto: "https", Address: "https://dns.example/dns-query", Domains: []string{"example.com"},
		}},
		Block: []string{"example.com"},
		Outbound: []types.NodeOutbound{{
			Name: "direct", Protocol: "shadowsocks", Port: 443, SpiderX: "/spider",
		}},
		Protocols: []types.Protocol{{
			Type: "vless", Port: 443, Enable: true, Transport: "xhttp", XhttpMode: "auto",
			Obfs: "http", ObfsPath: "/obfs",
		}},
		Total: 1,
	})
	if err != nil {
		t.Fatalf("queryServerProtocolConfigResponseToProtobuf() error = %v", err)
	}
	if protocols.Code != 200 || protocols.Data.Total != 1 || protocols.Data.IpStrategy != "prefer_ipv4" {
		t.Fatalf("protocols = %+v, want protobuf protocol-config envelope", protocols)
	}
	if len(protocols.Data.Dns) != 1 || protocols.Data.Dns[0].Address != "https://dns.example/dns-query" {
		t.Fatalf("dns = %+v, want strongly typed DNS configuration", protocols.Data.Dns)
	}
	if len(protocols.Data.Dns[0].Domains) != 1 || protocols.Data.Dns[0].Domains[0] != "example.com" {
		t.Fatalf("dns domains = %+v, want the resolver domains", protocols.Data.Dns[0].Domains)
	}
	if len(protocols.Data.Outbound) != 1 || protocols.Data.Outbound[0].SpiderX != "/spider" || protocols.Data.Outbound[0].Port != 443 {
		t.Fatalf("outbound = %+v, want strongly typed outbound configuration", protocols.Data.Outbound)
	}
	protocol := protocols.Data.Protocols[0]
	if protocol.XhttpMode != "auto" || protocol.Obfs != "http" || protocol.ObfsPath != "/obfs" || protocol.Port != 443 {
		t.Fatalf("protocol = %+v, want strongly typed protocol configuration", protocol)
	}
}

func TestProtocolConversionLeavesUnmodelledFieldsUnset(t *testing.T) {
	// Fields the local DTO does not carry must stay at their zero values rather
	// than being synthesised, so a node reading them can tell they are absent.
	converted := serverProtocolToProtobuf(types.Protocol{Type: "trojan", Port: 8443, Enable: true})
	if converted.Plugin != "" || converted.Alpn != nil || converted.Heartbeat != 0 {
		t.Fatalf("converted = %+v, want unmodelled fields unset", converted)
	}
	if converted.Type != "trojan" || converted.Port != 8443 || !converted.Enable {
		t.Fatalf("converted = %+v, want the modelled fields carried through", converted)
	}
}

func TestAcceptsProtobuf_RejectsZeroQuality(t *testing.T) {
	ctx := app.NewContext(0)
	ctx.Request.Header.Set("Accept", "application/protobuf;q=0, application/json")
	if acceptsProtobuf(ctx) {
		t.Fatal("acceptsProtobuf() = true, want false for q=0")
	}
}

func TestAcceptsProtobuf_ParsesParameters(t *testing.T) {
	ctx := app.NewContext(0)
	ctx.Request.Header.Set("Accept", "text/html, application/protobuf; charset=binary")
	if !acceptsProtobuf(ctx) {
		t.Fatal("acceptsProtobuf() = false, want true when protobuf is acceptable")
	}
}

func TestIfNoneMatchForRepresentation(t *testing.T) {
	if got := ifNoneMatchForRepresentation(`W/"abc"`, true); got != "" {
		t.Fatalf("protobuf ifNoneMatch = %q, want empty", got)
	}
	if got := ifNoneMatchForRepresentation(`W/"abc"`, false); got != `W/"abc"` {
		t.Fatalf("json ifNoneMatch = %q, want the client tag", got)
	}
}

func TestWriteServerProtobufWithETag(t *testing.T) {
	message := &serverv1.Result{Code: 200, Message: "success"}

	ctx := app.NewContext(0)
	if err := writeServerProtobufWithETag(ctx, message, ""); err != nil {
		t.Fatalf("writeServerProtobufWithETag() error = %v", err)
	}
	etag := string(ctx.Response.Header.Peek("ETag"))
	if etag == "" || string(ctx.Response.Header.ContentType()) != protobufContentType {
		t.Fatalf("response headers = {ETag:%q ContentType:%q}, want Protobuf content type and ETag", etag, string(ctx.Response.Header.ContentType()))
	}

	ctx = app.NewContext(0)
	if err := writeServerProtobufWithETag(ctx, message, etag); err != nil {
		t.Fatalf("writeServerProtobufWithETag() error = %v", err)
	}
	if got := ctx.Response.StatusCode(); got != 304 {
		t.Fatalf("status = %d, want 304", got)
	}
}
