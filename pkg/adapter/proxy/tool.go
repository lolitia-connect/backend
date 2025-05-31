package proxy

import (
	"github.com/perfect-panel/server/pkg/uuidx"
)

func GenerateShadowsocks2022Password(ss Shadowsocks, password string) (string, string) {
	// server key
	serverKey := ss.ServerKey
	if ss.Method == "2022-blake3-aes-128-gcm" {
		password = uuidx.UUIDToBase64(password, 16)
	} else {
		password = uuidx.UUIDToBase64(password, 32)
	}
	return serverKey, password
}
