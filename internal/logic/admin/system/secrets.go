package system

import (
	"encoding/json"

	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
)

const secretMask = "********"

func maskSecret(value string) string {
	if value == "" {
		return ""
	}
	return secretMask
}

func keepSettingsSecrets(category, key, requested, stored string) (string, error) {
	if (category == "verify" && key == "TurnstileSecret") || (category == "currency" && key == "AccessKey") {
		if requested != secretMask {
			return requested, nil
		}
		if stored == "" || stored == secretMask {
			return "", xerr.NewErrCode(xerr.InvalidParams)
		}
		return stored, nil
	}
	if category != "server" || key != "Outbound" {
		return requested, nil
	}
	var outbounds, previous []types.NodeOutbound
	if err := json.Unmarshal([]byte(requested), &outbounds); err != nil {
		return "", xerr.NewErrCode(xerr.InvalidParams)
	}
	needsStored := false
	for _, outbound := range outbounds {
		if outbound.Password == secretMask || outbound.UUID == secretMask {
			needsStored = true
			break
		}
	}
	if !needsStored {
		return requested, nil
	}
	if stored == "" || json.Unmarshal([]byte(stored), &previous) != nil {
		return "", xerr.NewErrCode(xerr.InvalidParams)
	}
	byName := make(map[string]int, len(previous))
	for index, outbound := range previous {
		if _, exists := byName[outbound.Name]; !exists {
			byName[outbound.Name] = index
		}
	}
	for index := range outbounds {
		outbound := &outbounds[index]
		if outbound.Password != secretMask && outbound.UUID != secretMask {
			continue
		}
		position, exists := byName[outbound.Name]
		if !exists {
			position = index
		}
		if position >= len(previous) {
			return "", xerr.NewErrCode(xerr.InvalidParams)
		}
		for _, secret := range []struct {
			requested *string
			stored    string
		}{
			{&outbound.Password, previous[position].Password},
			{&outbound.UUID, previous[position].UUID},
		} {
			if *secret.requested != secretMask {
				continue
			}
			if secret.stored == "" || secret.stored == secretMask {
				return "", xerr.NewErrCode(xerr.InvalidParams)
			}
			*secret.requested = secret.stored
		}
	}
	encoded, err := json.Marshal(outbounds)
	return string(encoded), err
}
