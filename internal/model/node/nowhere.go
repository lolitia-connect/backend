package node

import (
	"encoding/hex"
	"fmt"
	"strings"
)

func ProtocolCredential(protocol, credential string) (string, error) {
	if normalizeProtocolType(protocol) == "nowhere" {
		return NowhereSharedKey(credential)
	}
	return credential, nil
}

func NowhereSharedKey(credential string) (string, error) {
	if len(credential) == 36 {
		if credential[8] != '-' || credential[13] != '-' || credential[18] != '-' || credential[23] != '-' {
			return "", fmt.Errorf("invalid NW3 subscription credential")
		}
		credential = credential[:8] + credential[9:13] + credential[14:18] + credential[19:23] + credential[24:]
	}
	if len(credential) != 32 {
		return "", fmt.Errorf("invalid NW3 subscription credential")
	}
	var decoded [16]byte
	if _, err := hex.Decode(decoded[:], []byte(credential)); err != nil {
		return "", fmt.Errorf("invalid NW3 subscription credential")
	}
	return hex.EncodeToString(decoded[:]), nil
}

func upgradeStoredNowhere(protocol *Protocol) {
	if normalizeProtocolType(protocol.Type) != "nowhere" || (protocol.Version != 0 && protocol.Version != 1) {
		return
	}
	protocol.Type, protocol.Version = "nowhere", 3
	protocol.ALPN = []string{"nw3"}
	if strings.TrimSpace(protocol.Obfs) == "" {
		protocol.Obfs = "morph"
	}
}
