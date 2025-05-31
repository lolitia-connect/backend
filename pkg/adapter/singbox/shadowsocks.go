package singbox

import (
	"fmt"
	"strings"

	"github.com/perfect-panel/server/pkg/adapter/proxy"
)

type ShadowsocksOptions struct {
	ServerOptions
	Method        string `json:"method,omitempty"`
	Password      string `json:"password,omitempty"`
	Plugin        string `json:"plugin,omitempty"`
	PluginOptions string `json:"plugin_opts,omitempty"`
	Network       string `json:"network,omitempty"`
}

func ParseShadowsocks(data proxy.Proxy, uuid string) (*Proxy, error) {
	ss := data.Option.(proxy.Shadowsocks)

	password := uuid
	// SIP022 AEAD-2022 Ciphers
	if strings.Contains(ss.Method, "2022") {
		serverKey, userKey := proxy.GenerateShadowsocks2022Password(ss, uuid)
		password = fmt.Sprintf("%s:%s", serverKey, userKey)
	}

	p := &Proxy{
		Tag:  data.Name,
		Type: Shadowsocks,
		ShadowsocksOptions: &ShadowsocksOptions{
			ServerOptions: ServerOptions{
				Tag:        data.Name,
				Type:       Shadowsocks,
				Server:     data.Server,
				ServerPort: data.Port,
			},
			Method:   ss.Method,
			Password: password,
			Network:  "tcp",
		},
	}
	return p, nil
}
