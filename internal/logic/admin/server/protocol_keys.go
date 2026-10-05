package server

import (
	"strings"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/pkg/errors"
)

// applyGeneratedProtocolKeys fills in the listener secrets a protocol needs but
// the administrator did not supply. Reality keys are generated for every
// protocol type that supports the reality security mode, not only vless, so a
// trojan reality server is provisioned the same way as a vless one.
func applyGeneratedProtocolKeys(protocol *node.Protocol) error {
	if strings.EqualFold(strings.TrimSpace(protocol.Security), "reality") {
		if protocol.RealityPublicKey == "" || protocol.RealityPrivateKey == "" || protocol.RealityShortId == "" {
			public, private, err := tool.Curve25519Genkey(false, "")
			if err != nil {
				return errors.Wrapf(err, "generate reality key error")
			}
			protocol.RealityPublicKey = public
			protocol.RealityPrivateKey = private
			protocol.RealityShortId = tool.GenerateShortID(private)
		}
		if protocol.RealityServerAddr == "" {
			protocol.RealityServerAddr = protocol.SNI
		}
		if protocol.RealityServerPort == 0 {
			protocol.RealityServerPort = 443
		}
	}

	// ShadowSocks 2022 server key generation.
	if protocol.Type == "shadowsocks" && strings.Contains(protocol.Cipher, "2022") {
		length := 32
		switch protocol.Cipher {
		case "2022-blake3-aes-128-gcm":
			length = 16
		}
		if len(protocol.ServerKey) != length {
			protocol.ServerKey = tool.GenerateCipher(protocol.ServerKey, length)
		}
	}
	return nil
}
