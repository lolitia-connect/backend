package server

import (
	"fmt"
	"testing"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestLegacyNodeConfigProjectsProtocolSettingsAndCachesResponse(t *testing.T) {
	cases := []struct {
		protocol node.Protocol
		section  string
		want     map[string]any
	}{
		{node.Protocol{Type: ShadowSocks, Cipher: "2022-blake3-aes-128-gcm", ServerKey: "sixteen-byte-key"}, "", map[string]any{"method": "2022-blake3-aes-128-gcm", "server_key": "c2l4dGVlbi1ieXRlLWtleQ=="}},
		{node.Protocol{Type: Vless, Transport: "ws", Path: "/edge"}, "transport_config", map[string]any{"path": "/edge"}},
		{node.Protocol{Type: Vmess, Transport: "grpc", ServiceName: "vmess-edge"}, "transport_config", map[string]any{"service_name": "vmess-edge"}},
		{node.Protocol{Type: Trojan, Security: "tls", SNI: "trojan.example.test"}, "security_config", map[string]any{"sni": "trojan.example.test"}},
		{node.Protocol{Type: AnyTLS, PaddingScheme: "stop=8"}, "security_config", map[string]any{"padding_scheme": "stop=8"}},
		{node.Protocol{Type: Tuic, Security: "tls", AllowInsecure: true}, "security_config", map[string]any{"allow_insecure": true}},
		{node.Protocol{Type: Hysteria, HopPorts: "4400-4410", HopInterval: 30}, "", map[string]any{"hop_ports": "4400-4410", "hop_interval": float64(30)}},
	}
	protocols := []node.Protocol{{Type: "nowhere", Id: "edge", Enable: true}, {Type: Vless, Id: "disabled"}}
	for i := range cases {
		protocol := &cases[i].protocol
		protocol.Id, protocol.Enable, protocol.Port = "edge", true, 8443
		protocols = append(protocols, *protocol)
	}
	service, server, cache := nodeLogicFixture(t, protocols...)
	service.Config.Node.NodePullInterval = 11
	service.Config.Node.NodePushInterval = 22
	for _, tc := range cases {
		t.Run(tc.protocol.Type, func(t *testing.T) {
			ctx := t.Context()
			request := &types.GetServerConfigRequest{ServerCommon: types.ServerCommon{ServerId: server.ID, Protocol: tc.protocol.Type, ProtocolId: tc.protocol.Id}}
			logic := NewGetServerConfigLogic(ctx, service, RequestMeta{})
			response, err := logic.GetServerConfig(request)
			require.NoError(t, err)
			require.Equal(t, types.ServerBasic{PullInterval: 11, PushInterval: 22}, response.Basic)
			require.Equal(t, tc.protocol.Type, response.Protocol)
			fields, ok := response.Config.(map[string]any)
			require.True(t, ok, "expected a protocol object, got %T", response.Config)
			require.Equal(t, float64(8443), fields["port"])
			if tc.section != "" {
				fields, ok = fields[tc.section].(map[string]any)
				require.True(t, ok, "missing %s", tc.section)
			}
			for key, value := range tc.want {
				require.Equal(t, value, fields[key], key)
			}

			etag := logic.ResponseMeta().Headers["ETag"]
			require.NotEmpty(t, etag)
			if tc.protocol.Type == Vless {
				service.Config.Node.NodePullInterval = 99
				t.Cleanup(func() { service.Config.Node.NodePullInterval = 11 })
				cachedLogic := NewGetServerConfigLogic(ctx, service, RequestMeta{})
				cached, err := cachedLogic.GetServerConfig(request)
				require.NoError(t, err)
				require.Equal(t, response, cached)
				require.Equal(t, etag, cachedLogic.ResponseMeta().Headers["ETag"])
				_, err = NewGetServerConfigLogic(ctx, service, RequestMeta{IfNoneMatch: etag}).GetServerConfig(request)
				require.ErrorIs(t, err, xerr.StatusNotModified)

				cache.Del(fmt.Sprintf("%s%d:vless:edge", node.ServerConfigCacheKey, server.ID))
				freshLogic := NewGetServerConfigLogic(ctx, service, RequestMeta{IfNoneMatch: etag})
				fresh, err := freshLogic.GetServerConfig(request)
				require.NoError(t, err)
				require.EqualValues(t, 99, fresh.Basic.PullInterval)
				require.NotEqual(t, etag, freshLogic.ResponseMeta().Headers["ETag"])
			}
		})
	}

	for _, tc := range []struct{ protocol, instance, message string }{
		{"nowhere", "edge", "use /v2/server/{server_id}"},
		{Vless, "disabled", "not found or disabled"},
	} {
		request := &types.GetServerConfigRequest{ServerCommon: types.ServerCommon{ServerId: server.ID, Protocol: tc.protocol, ProtocolId: tc.instance}}
		_, err := NewGetServerConfigLogic(t.Context(), service, RequestMeta{}).GetServerConfig(request)
		require.ErrorContains(t, err, tc.message)
		require.False(t, cache.Exists(fmt.Sprintf("%s%d:%s:%s", node.ServerConfigCacheKey, server.ID, tc.protocol, tc.instance)))
	}
}

func TestFlatNodeConfigMergesOverrideAndFiltersRuntimeProtocols(t *testing.T) {
	ctx := t.Context()
	tls := node.Protocol{Type: Vless, Id: "tls", Port: 443, Enable: true, Transport: "tcp", Security: "tls", SNI: "node.example.test", CertMode: "http", EchEnable: true, EchServerName: "ech.example.test"}
	service, server, _ := nodeLogicFixture(t, tls,
		node.Protocol{Type: Vless, Id: "plain", Port: 8443, Enable: true, Transport: "tcp"},
		node.Protocol{Type: Vless, Id: "disabled", Port: 8444},
		node.Protocol{Type: Trojan, Id: "invalid", Port: 9443, Enable: true, Security: "tls"},
	)
	service.Config.Node = config.NodeConfig{
		IPStrategy: "prefer_ipv4", DNS: []config.NodeDNS{{Proto: "udp", Address: "192.0.2.53:53"}},
		Block: []string{"geosite:ads"}, NodePullInterval: 11, NodePushInterval: 22, TrafficReportThreshold: 4096,
	}
	logic := NewQueryServerProtocolConfigLogic(ctx, service)
	request := &types.QueryServerConfigRequest{ServerID: server.ID}
	global, err := logic.QueryServerProtocolConfig(request)
	require.NoError(t, err)
	require.Equal(t, "prefer_ipv4", global.IPStrategy)
	require.Len(t, global.DNS, 1)
	require.EqualValues(t, 2, global.Total)

	strategy, dns := "prefer_ipv6", "[]"
	require.NoError(t, service.Store.Node().SaveServerConfigOverride(ctx, &node.ServerConfigOverride{ServerId: server.ID, IPStrategy: &strategy, DNS: &dns}))
	request.Protocols = []string{"vless:tls", "vless:disabled", "trojan"}
	effective, err := logic.QueryServerProtocolConfig(request)
	require.NoError(t, err)
	require.Equal(t, "prefer_ipv6", effective.IPStrategy)
	require.Empty(t, effective.DNS)
	require.Equal(t, []string{"geosite:ads"}, effective.Block)
	require.EqualValues(t, 11, effective.PullInterval)
	require.EqualValues(t, 22, effective.PushInterval)
	require.EqualValues(t, 4096, effective.TrafficReportThreshold)
	require.EqualValues(t, 1, effective.Total)
	require.Len(t, effective.Protocols, 1)
	protocol := effective.Protocols[0]
	require.Equal(t, "tls", protocol.Id)
	require.Equal(t, tls.SNI, protocol.SNI)
	require.False(t, protocol.EchEnable)
	require.Empty(t, protocol.EchServerName)

	request.ServerID = server.ID + 1
	_, err = logic.QueryServerProtocolConfig(request)
	require.Error(t, err)
}
