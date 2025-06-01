package proxy

import (
	"encoding/base64"
	"github.com/perfect-panel/server/pkg/uuidx"
)

func GenerateShadowsocks2022Password(ss Shadowsocks, password string) (string, string) {
	if ss.Method == "2022-blake3-aes-128-gcm" {
		password = uuidx.UUIDToBase64(password, 16)
	} else {
		password = uuidx.UUIDToBase64(password, 32)
	}
	return base64.StdEncoding.EncodeToString([]byte(ss.ServerKey)), password
}
