package v2rayn

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/perfect-panel/server/pkg/adapter/proxy"
)

type v2rayShareLink struct {
	Ps            string `json:"ps"`
	Add           string `json:"add"`
	Port          string `json:"port"`
	ID            string `json:"id"`
	Aid           string `json:"aid"`
	Net           string `json:"net"`
	Type          string `json:"type"`
	Host          string `json:"host"`
	SNI           string `json:"sni"`
	Path          string `json:"path"`
	TLS           string `json:"tls"`
	Flow          string `json:"flow,omitempty"`
	Alpn          string `json:"alpn,omitempty"`
	AllowInsecure bool   `json:"allowInsecure,omitempty"`
	Fingerprint   string `json:"fp,omitempty"`
	PublicKey     string `json:"pbk,omitempty"`
	ShortId       string `json:"sid,omitempty"`
	SpiderX       string `json:"spx,omitempty"`
	V             string `json:"v"`
}
type V2rayN struct {
	proxy.Adapter
}

func NewV2rayN(adapter proxy.Adapter) *V2rayN {
	return &V2rayN{
		Adapter: adapter,
	}
}
func (m *V2rayN) Build(uuid string) []byte {
	uri := ""
	for _, p := range m.Proxies {
		switch p.Protocol {
		case "shadowsocks":
			uri += m.buildShadowsocks(uuid, p) + "\r\n"
		case "vmess":
			uri += m.buildVmess(uuid, p) + "\r\n"
		case "vless":
			uri += m.buildVless(uuid, p) + "\r\n"
		case "trojan":
			uri += m.buildTrojan(uuid, p) + "\r\n"
		case "hysteria2":
			uri += m.buildHysteria2(uuid, p) + "\r\n"
		case "tuic":
			uri += m.buildTuic(uuid, p) + "\r\n"
		}
	}
	result := base64.StdEncoding.EncodeToString([]byte(uri))

	return []byte(result)
}

func (m *V2rayN) buildShadowsocks(uuid string, data proxy.Proxy) string {
	ss, ok := data.Option.(proxy.Shadowsocks)
	if !ok {
		return ""
	}

	password := uuid
	// SIP022 AEAD-2022 Ciphers
	if strings.Contains(ss.Method, "2022") {
		serverKey, userKey := proxy.GenerateShadowsocks2022Password(ss, uuid)
		password = fmt.Sprintf("%s:%s", serverKey, userKey)
	}

	// sip002
	u := &url.URL{
		Scheme: "ss",
		// 还没有写 2022 的
		User:     url.User(strings.TrimSuffix(base64.URLEncoding.EncodeToString([]byte(ss.Method+":"+password)), "=")),
		Host:     net.JoinHostPort(data.Server, strconv.Itoa(data.Port)),
		Fragment: data.Name,
	}
	return u.String()
}

func (m *V2rayN) buildTrojan(uuid string, data proxy.Proxy) string {
	trojan := data.Option.(proxy.Trojan)
	transportConfig := trojan.TransportConfig
	securityConfig := trojan.SecurityConfig

	var query = make(url.Values)
	setQuery(&query, "type", trojan.Transport)
	setQuery(&query, "security", trojan.Security)

	switch trojan.Transport {
	case "ws", "http", "httpupgrade":
		setQuery(&query, "path", transportConfig.Path)
		setQuery(&query, "host", transportConfig.Host)
	case "grpc":
		setQuery(&query, "serviceName", transportConfig.ServiceName)
	case "meek":
		setQuery(&query, "url", transportConfig.Host)
	}

	setQuery(&query, "sni", securityConfig.SNI)
	setQuery(&query, "fp", securityConfig.Fingerprint)
	setQuery(&query, "pbk", securityConfig.RealityPublicKey)
	setQuery(&query, "sid", securityConfig.RealityShortId)

	if securityConfig.AllowInsecure {
		setQuery(&query, "allowInsecure", "1")
	}

	u := &url.URL{
		Scheme:   "trojan",
		User:     url.User(uuid),
		Host:     net.JoinHostPort(data.Server, strconv.Itoa(data.Port)),
		RawQuery: query.Encode(),
		Fragment: data.Name,
	}
	return u.String()
}

func (m *V2rayN) buildVmess(uuid string, data proxy.Proxy) string {
	vmess := data.Option.(proxy.Vmess)

	transport := vmess.TransportConfig

	securityConfig := vmess.SecurityConfig

	var s = v2rayShareLink{
		V:    "2",
		Add:  data.Server,
		Port: fmt.Sprint(data.Port),
		ID:   uuid,
		Aid:  "0",
	}

	switch vmess.Transport {
	case "websocket":
		s.Net = "ws"
		s.Path = transport.Path
		s.Host = transport.Host
	case "grpc":
		s.Net = "grpc"
		s.Path = transport.ServiceName
	case "httpupgrade":
		s.Net = "http"
		s.Path = transport.Path
		s.Host = transport.Host
	}

	if vmess.Security == "tls" {
		s.TLS = "tls"
		s.SNI = securityConfig.SNI
		s.AllowInsecure = securityConfig.AllowInsecure
		s.Fingerprint = securityConfig.Fingerprint
	}
	b, _ := json.Marshal(s)
	return "vmess://" + strings.TrimSuffix(base64.StdEncoding.EncodeToString(b), "=")
}

func (m *V2rayN) buildVless(uuid string, data proxy.Proxy) string {
	vless := data.Option.(proxy.Vless)
	transportConfig := vless.TransportConfig
	securityConfig := vless.SecurityConfig

	var query = make(url.Values)
	setQuery(&query, "flow", vless.Flow)
	setQuery(&query, "security", vless.Security)

	switch vless.Transport {
	case "websocket":
		setQuery(&query, "type", "ws")
		setQuery(&query, "host", transportConfig.Host)
		setQuery(&query, "path", transportConfig.Path)

	case "http2", "httpupgrade":
		setQuery(&query, "type", vless.Transport)
		setQuery(&query, "path", transportConfig.Path)
		setQuery(&query, "host", transportConfig.Host)
	case "grpc":
		setQuery(&query, "type", "grpc")
		setQuery(&query, "serviceName", transportConfig.ServiceName)
	}

	if vless.Security == "tls" {
		setQuery(&query, "sni", securityConfig.SNI)
		setQuery(&query, "fp", securityConfig.Fingerprint)
	} else if vless.Security == "reality" {
		setQuery(&query, "pbk", securityConfig.RealityPublicKey)
		setQuery(&query, "sid", securityConfig.RealityShortId)
		setQuery(&query, "sni", securityConfig.SNI)
		setQuery(&query, "fp", securityConfig.Fingerprint)
		setQuery(&query, "servername", securityConfig.SNI)
		setQuery(&query, "spx", "/")

	}

	u := url.URL{
		Scheme:   "vless",
		User:     url.User(uuid),
		Host:     net.JoinHostPort(data.Server, fmt.Sprint(data.Port)),
		RawQuery: query.Encode(),
		Fragment: data.Name,
	}
	return u.String()
}

func (m *V2rayN) buildHysteria2(uuid string, data proxy.Proxy) string {
	hysteria2 := data.Option.(proxy.Hysteria2)

	var query = make(url.Values)

	setQuery(&query, "sni", hysteria2.SecurityConfig.SNI)

	if hysteria2.SecurityConfig.AllowInsecure {
		setQuery(&query, "insecure", "1")
	}

	if hp := strings.TrimSpace(hysteria2.HopPorts); hp != "" {
		setQuery(&query, "mport", hp)
	}

	if hysteria2.ObfsPassword != "" {
		setQuery(&query, "obfs", "salamander")
		setQuery(&query, "obfs-password", hysteria2.ObfsPassword)
	}

	u := &url.URL{
		Scheme:   "hysteria2",
		User:     url.User(uuid),
		Host:     net.JoinHostPort(data.Server, strconv.Itoa(data.Port)),
		RawQuery: query.Encode(),
		Fragment: data.Name,
	}
	return u.String()
}

func (m *V2rayN) buildTuic(uuid string, data proxy.Proxy) string {
	tuic := data.Option.(proxy.Tuic)
	var query = make(url.Values)

	setQuery(&query, "congestion_control", "bbr")

	if tuic.SecurityConfig.SNI == "" {
		setQuery(&query, "sni", tuic.SecurityConfig.SNI)
	} else {
		setQuery(&query, "disable_sni", "1")
	}
	if tuic.SecurityConfig.AllowInsecure {
		setQuery(&query, "allow_insecure", "1")
	}

	u := &url.URL{
		Scheme:   "tuic",
		User:     url.User(uuid + ":" + uuid),
		Host:     net.JoinHostPort(data.Server, strconv.Itoa(data.Port)),
		RawQuery: query.Encode(),
		Fragment: data.Name,
	}
	return u.String()
}

func setQuery(q *url.Values, k, v string) {
	if v != "" {
		q.Set(k, v)
	}
}
