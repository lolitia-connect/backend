package surge

import (
	"fmt"
	"strings"

	"github.com/perfect-panel/server/pkg/adapter/proxy"
)

func buildShadowsocks(data proxy.Proxy, uuid string) string {
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

	addr := fmt.Sprintf("%s=ss, %s, %d", data.Name, data.Server, data.Port)
	config := []string{
		addr,
		fmt.Sprintf("encrypt-method=%s", ss.Method),
		fmt.Sprintf("password=%s", password),
		"tfo=true",
		"udp-relay=true",
	}
	return strings.Join(config, ",") + "\r\n"
}
