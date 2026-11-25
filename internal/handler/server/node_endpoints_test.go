package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/alicebob/miniredis/v2"
	hertz "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	serverv1 "github.com/perfect-panel/server/api/server/v1"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/enttest"
	serverlogic "github.com/perfect-panel/server/internal/logic/server"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite"
)

const testNodeSecret = "fixture-secret"

type nodeHTTPFixture struct {
	service  *svc.ServiceContext
	router   *hertz.Hertz
	serverID int64
}

func newNodeHTTPFixture(t *testing.T) nodeHTTPFixture {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	cache := miniredis.RunT(t)
	rds := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { _ = rds.Close() })
	service := &svc.ServiceContext{Store: repository.NewStore(client, rds), Redis: rds}
	service.Config.Node.NodeSecret = testNodeSecret
	service.Config.Node.NodePullInterval = 30
	data := &node.Server{}
	require.NoError(t, data.MarshalProtocols([]node.Protocol{
		{Type: "vless", Id: "edge", Enable: true, Port: 443, Transport: "tcp", Security: "tls", SNI: "edge.example.test", CertMode: "self"},
		{Type: "vless", Id: "alt", Enable: true, Port: 8443, Transport: "tcp"},
		{Type: "vless", Id: "off", Port: 9443},
	}))
	server := client.Server.Create().SetName("HTTP node").SetProtocols(data.Protocols).SaveX(t.Context())
	router := hertz.New()
	v1 := router.Group("/v1/server", ServerMiddleware(service))
	v1.GET("/config", GetServerConfigHandler(service))
	v1.GET("/user", GetServerUserListHandler(service))
	v1.GET("/alivelist", GetServerAliveListHandler(service))
	v1.POST("/device", ClaimServerDeviceHandler(service))
	v1.POST("/status", ServerPushStatusHandler(service))
	v1.POST("/online", PushOnlineUsersHandler(service))
	v1.POST("/push", ServerPushUserTrafficHandler(service))
	router.GET("/v2/server/:server_id", QueryServerProtocolConfigHandler(service))
	return nodeHTTPFixture{service: service, router: router, serverID: server.ID}
}

func (f nodeHTTPFixture) v1(endpoint string) string {
	return fmt.Sprintf("/v1/server/%s?server_id=%d&protocol=vless&protocol_id=edge&secret_key=%s", endpoint, f.serverID, testNodeSecret)
}

func (f nodeHTTPFixture) seedServedUser(t *testing.T) *ent.UserSubscribe {
	t.Helper()
	ctx := t.Context()
	client := f.service.Store.Ent()
	owner := client.User.Create().SetPassword("test-password").SaveX(ctx)
	plan := client.Subscribe.Create().SetDeviceLimit(1).SaveX(ctx)
	subscription := client.UserSubscribe.Create().SetUserID(owner.ID).SetOrderID(1).SetSubscribeID(plan.ID).
		SetUUID("00000000-0000-4000-8000-000000000001").SetStatus(1).SaveX(ctx)
	payload, err := json.Marshal(types.GetServerUserListResponse{Users: []types.ServerUser{{Id: subscription.ID, UUID: subscription.UUID, DeviceLimit: 1}}})
	require.NoError(t, err)
	key := fmt.Sprintf("%s%d:vless:edge", node.ServerUserListCacheKey, f.serverID)
	generation, err := node.ServerCacheGeneration(ctx, f.service.Redis, f.serverID)
	require.NoError(t, err)
	published, err := node.PublishServerCache(ctx, f.service.Redis, f.serverID, key, payload, generation)
	require.NoError(t, err)
	require.True(t, published)
	return subscription
}

func (f nodeHTTPFixture) request(method, target string, payload []byte, headers ...ut.Header) *ut.ResponseRecorder {
	var body *ut.Body
	if payload != nil {
		body = &ut.Body{Body: bytes.NewReader(payload), Len: len(payload)}
	}
	return ut.PerformRequest(f.router.Engine, method, target, body, headers...)
}

func requireNodeResultCode(t *testing.T, response *ut.ResponseRecorder, binary bool, want uint32) {
	t.Helper()
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	if binary {
		require.Equal(t, protobufContentType, string(response.Header().ContentType()))
		var result serverv1.Result
		require.NoError(t, proto.Unmarshal(response.Body.Bytes(), &result))
		require.Equal(t, want, result.GetCode())
		return
	}
	var result struct{ Code uint32 }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, want, result.Code, response.Body.String())
}

func TestNodeReportRoutesIngestPayloadsAndRejectMalformedRequests(t *testing.T) {
	f := newNodeHTTPFixture(t)
	ctx := t.Context()
	jsonType := ut.Header{Key: "Content-Type", Value: "application/json"}
	protoType := ut.Header{Key: "Content-Type", Value: protobufContentType}
	protoAccept := ut.Header{Key: "Accept", Value: protobufContentType}
	pin := strings.Repeat("ab", 32)
	status, err := proto.Marshal(&serverv1.PushServerStatusRequest{Cpu: 0.25, Mem: 0.5, Disk: 0.75, UpdatedAt: 1234})
	require.NoError(t, err)
	response := f.request(http.MethodPost, f.v1("status"), status, protoType, protoAccept, ut.Header{Key: certificateSHA256Header, Value: pin})
	requireNodeResultCode(t, response, true, 200)
	cached, err := f.service.Store.Node().StatusCache(ctx, f.serverID)
	require.NoError(t, err)
	require.Equal(t, node.Status{Cpu: 0.25, Mem: 0.5, Disk: 0.75, UpdatedAt: 1234}, cached)
	server, err := f.service.Store.Node().FindOneServer(ctx, f.serverID)
	require.NoError(t, err)
	protocols, err := server.UnmarshalProtocols()
	require.NoError(t, err)
	require.Equal(t, pin, protocols[0].CertPinSHA256)

	subscription := f.seedServedUser(t)
	online := fmt.Appendf(nil, `{"users":[{"uid":%d,"ip":"::ffff:192.0.2.1"}]}`, subscription.ID)
	response = f.request(http.MethodPost, f.v1("online"), online, jsonType)
	requireNodeResultCode(t, response, false, 200)
	snapshot, err := f.service.Store.Node().OnlineUserSubscribe(ctx, f.serverID, "vless:edge")
	require.NoError(t, err)
	require.Equal(t, node.OnlineUserSubscribe{subscription.ID: {"192.0.2.1"}}, snapshot)

	traffic, err := proto.Marshal(&serverv1.PushUserTrafficRequest{Traffic: []*serverv1.UserTraffic{{UserId: subscription.ID, Upload: 7, Download: 19}}})
	require.NoError(t, err)
	response = f.request(http.MethodPost, f.v1("push"), traffic, protoType, protoAccept)
	requireNodeResultCode(t, response, true, 200)

	for _, endpoint := range []string{"status", "online", "push"} {
		t.Run(endpoint, func(t *testing.T) {
			target := f.v1(endpoint)
			requireNodeResultCode(t, f.request(http.MethodPost, target, []byte("{"), jsonType), false, xerr.InvalidParams)
			requireNodeResultCode(t, f.request(http.MethodPost, target, []byte{0x80}, protoType, protoAccept), true, xerr.InvalidParams)
			badID := strings.Replace(target, fmt.Sprintf("server_id=%d", f.serverID), "server_id=bad", 1)
			requireNodeResultCode(t, f.request(http.MethodPost, badID, []byte(`{}`), jsonType), false, xerr.InvalidParams)
			missingInstance := strings.Replace(target, "&protocol_id=edge", "", 1)
			requireNodeResultCode(t, f.request(http.MethodPost, missingInstance, []byte(`{}`), jsonType), false, xerr.InvalidParams)
		})
	}
	forbidden := f.request(http.MethodPost, strings.Replace(f.v1("status"), testNodeSecret, "wrong", 1), []byte(`{"cpu":0.9}`), jsonType)
	require.Equal(t, http.StatusForbidden, forbidden.Code)
	current, err := f.service.Store.Node().StatusCache(ctx, f.serverID)
	require.NoError(t, err)
	require.Equal(t, cached, current)
	unchanged, err := f.service.Store.Node().OnlineUserSubscribe(ctx, f.serverID, "vless:edge")
	require.NoError(t, err)
	require.Equal(t, snapshot, unchanged)
	require.NoError(t, serverlogic.NewTrafficAggregator(f.service).Flush(ctx, time.Now().Add(2*time.Minute)))
	stored := f.service.Store.Ent().UserSubscribe.GetX(ctx, subscription.ID)
	require.EqualValues(t, 7, stored.Upload)
	require.EqualValues(t, 19, stored.Download)
}

func TestDeviceRoutesEnforceLeaseAndReturnClientErrors(t *testing.T) {
	f := newNodeHTTPFixture(t)
	subscription := f.seedServedUser(t)
	jsonType := ut.Header{Key: "Content-Type", Value: "application/json"}
	protoAccept := ut.Header{Key: "Accept", Value: protobufContentType}
	body := fmt.Appendf(nil, `{"uid":%d,"ip":"192.0.2.1"}`, subscription.ID)
	response := f.request(http.MethodPost, f.v1("device"), body, jsonType)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "no-store", string(response.Header().Peek("Cache-Control")))
	var admitted types.ClaimServerDeviceResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &admitted))
	require.True(t, admitted.Allowed)
	require.Equal(t, int64(node.Expiry.Seconds()), admitted.TTLSeconds)

	body = fmt.Appendf(nil, `{"uid":%d,"ip":"192.0.2.2"}`, subscription.ID)
	response = f.request(http.MethodPost, f.v1("device"), body, jsonType, protoAccept)
	require.Equal(t, http.StatusOK, response.Code)
	var denied serverv1.ClaimServerDeviceResponse
	require.NoError(t, proto.Unmarshal(response.Body.Bytes(), &denied))
	require.EqualValues(t, 200, denied.GetCode())
	require.NotNil(t, denied.GetData())
	require.False(t, denied.GetData().GetAllowed())

	response = f.request(http.MethodGet, f.v1("alivelist"), nil, protoAccept)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "no-store", string(response.Header().Peek("Cache-Control")))
	var alive serverv1.GetServerAliveListResponse
	require.NoError(t, proto.Unmarshal(response.Body.Bytes(), &alive))
	require.Equal(t, map[int64]int64{subscription.ID: 1}, alive.GetData().GetAlive())

	wrongInstance := strings.Replace(f.v1("device"), "protocol_id=edge", "protocol_id=alt", 1)
	requireNodeResultCode(t, f.request(http.MethodPost, wrongInstance, body, jsonType, protoAccept), true, xerr.InvalidAccess)
	requireNodeResultCode(t, f.request(http.MethodPost, f.v1("device"), []byte(`{"ip":"192.0.2.2"}`), jsonType), false, xerr.InvalidParams)
	requireNodeResultCode(t, f.request(http.MethodPost, f.v1("device"), []byte{0x80}, ut.Header{Key: "Content-Type", Value: protobufContentType}, protoAccept), true, xerr.InvalidParams)
	badAliveQuery := strings.Replace(f.v1("alivelist"), "&protocol_id=edge", "", 1)
	requireNodeResultCode(t, f.request(http.MethodGet, badAliveQuery, nil), false, xerr.InvalidParams)
}

func TestNodeConfigAndUserRoutesNegotiateRepresentations(t *testing.T) {
	f := newNodeHTTPFixture(t)
	subscription := f.seedServedUser(t)
	protoAccept := ut.Header{Key: "Accept", Value: protobufContentType}
	for _, endpoint := range []string{"config", "user"} {
		t.Run(endpoint, func(t *testing.T) {
			target := f.v1(endpoint)
			response := f.request(http.MethodGet, target, nil)
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, "Accept", string(response.Header().Peek("Vary")))
			jsonTag := string(response.Header().Peek("ETag"))
			require.NotEmpty(t, jsonTag)
			if endpoint == "config" {
				var config types.GetServerConfigResponse
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &config))
				require.Equal(t, "vless", config.Protocol)
				require.EqualValues(t, 30, config.Basic.PullInterval)
			} else {
				var users types.GetServerUserListResponse
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &users))
				require.Equal(t, []types.ServerUser{{Id: subscription.ID, UUID: subscription.UUID, DeviceLimit: 1}}, users.Users)
			}
			conditional := ut.Header{Key: "If-None-Match", Value: jsonTag}
			require.Equal(t, http.StatusNotModified, f.request(http.MethodGet, target, nil, conditional).Code)

			response = f.request(http.MethodGet, target, nil, conditional, protoAccept)
			require.Equal(t, http.StatusOK, response.Code, "a JSON ETag cannot validate protobuf")
			require.Equal(t, protobufContentType, string(response.Header().ContentType()))
			binaryTag := string(response.Header().Peek("ETag"))
			require.NotEmpty(t, binaryTag)
			require.NotEqual(t, jsonTag, binaryTag)
			if endpoint == "config" {
				var config serverv1.GetServerConfigResponse
				require.NoError(t, proto.Unmarshal(response.Body.Bytes(), &config))
				require.EqualValues(t, 200, config.GetCode())
				require.EqualValues(t, 443, config.GetData().GetConfig().GetFields()["port"].GetNumberValue())
			} else {
				var users serverv1.GetServerUserListResponse
				require.NoError(t, proto.Unmarshal(response.Body.Bytes(), &users))
				require.Len(t, users.GetData().GetUsers(), 1)
				require.Equal(t, subscription.ID, users.GetData().GetUsers()[0].GetId())
			}
			unknownServer := strings.Replace(target, fmt.Sprintf("server_id=%d", f.serverID), fmt.Sprintf("server_id=%d", f.serverID+1), 1)
			require.Equal(t, http.StatusNotFound, f.request(http.MethodGet, unknownServer, nil).Code)
			badQuery := strings.Replace(target, "&protocol=vless", "", 1)
			requireNodeResultCode(t, f.request(http.MethodGet, badQuery, nil), false, xerr.InvalidParams)
		})
	}
}

func TestFlatNodeConfigRouteRequiresSecretAndSupportsInstanceSelectors(t *testing.T) {
	f := newNodeHTTPFixture(t)
	protoAccept := ut.Header{Key: "Accept", Value: protobufContentType}
	target := fmt.Sprintf("/v2/server/%d?secret_key=%s&protocols=vless:off&protocols[]=vless:alt", f.serverID, testNodeSecret)
	response := f.request(http.MethodGet, target, nil)
	require.Equal(t, http.StatusOK, response.Code)
	var config struct {
		Code uint32
		Data types.QueryServerConfigResponse
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &config))
	require.EqualValues(t, 200, config.Code)
	require.EqualValues(t, 1, config.Data.Total)
	require.Len(t, config.Data.Protocols, 1)
	require.Equal(t, "alt", config.Data.Protocols[0].Id)

	jsonTag := string(response.Header().Peek("ETag"))
	require.NotEmpty(t, jsonTag)
	conditional := ut.Header{Key: "If-None-Match", Value: jsonTag}
	require.Equal(t, http.StatusNotModified, f.request(http.MethodGet, target, nil, conditional).Code)
	response = f.request(http.MethodGet, target, nil, conditional, protoAccept)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, protobufContentType, string(response.Header().ContentType()))
	var binary serverv1.QueryServerProtocolConfigResponse
	require.NoError(t, proto.Unmarshal(response.Body.Bytes(), &binary))
	require.EqualValues(t, 200, binary.GetCode())
	require.Len(t, binary.GetData().GetProtocols(), 1)
	require.EqualValues(t, 8443, binary.GetData().GetProtocols()[0].GetPort())
	binaryTag := string(response.Header().Peek("ETag"))
	require.NotEmpty(t, binaryTag)
	require.NotEqual(t, jsonTag, binaryTag)
	response = f.request(http.MethodGet, target, nil, protoAccept, ut.Header{Key: "If-None-Match", Value: binaryTag})
	require.Equal(t, http.StatusNotModified, response.Code)
	require.Zero(t, response.Body.Len())

	unauthorized := strings.Replace(target, testNodeSecret, "wrong", 1)
	response = f.request(http.MethodGet, unauthorized, nil, protoAccept)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	var rejected serverv1.Result
	require.NoError(t, proto.Unmarshal(response.Body.Bytes(), &rejected))
	require.EqualValues(t, http.StatusUnauthorized, rejected.GetCode())
	badPath := fmt.Sprintf("/v2/server/bad?secret_key=%s", testNodeSecret)
	require.Equal(t, http.StatusBadRequest, f.request(http.MethodGet, badPath, nil).Code)
}
