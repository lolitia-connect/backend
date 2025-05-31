package loon

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/perfect-panel/server/pkg/adapter/proxy"
)

func buildShadowsocks(data proxy.Proxy, password string) string {
	shadowsocks := data.Option.(proxy.Shadowsocks)
	// If the method is 2022-blake3-chacha20-poly1305, it means that the server is a relay server
	if shadowsocks.Method == "2022-blake3-chacha20-poly1305" {
		return ""
	}

	if strings.Contains(shadowsocks.Method, "2022") {
		serverKey, userKey := proxy.GenerateShadowsocks2022Password(shadowsocks, password)
		password = fmt.Sprintf("%s:%s", serverKey, userKey)
	}

	configs := []string{
		fmt.Sprintf("%s=Shadowsocks", data.Name),
		data.Server,
		strconv.Itoa(data.Port),
		shadowsocks.Method,
		password,
		"fast-open=false",
		"udp=true",
	}
	uri := strings.Join(configs, ",")
	return uri + "\r\n"
}
