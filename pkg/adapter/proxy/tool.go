package proxy

import (
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
)

func GenerateShadowsocks2022Password(ss Shadowsocks, password string) (string, string) {
	// server key
	var serverKey string
	if ss.Method == "2022-blake3-aes-128-gcm" {
		serverKey = tool.GenerateCipher(ss.ServerKey, 16)
		password = uuidx.UUIDToBase64(password, 16)
	} else {
		serverKey = tool.GenerateCipher(ss.ServerKey, 32)
		password = uuidx.UUIDToBase64(password, 32)
	}
	return serverKey, password
}
