package httpserver

import (
	"fmt"
	"net"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	appconfig "github.com/perfect-panel/server/internal/config"
)

func ParseTrustedProxies(entries []string) ([]*net.IPNet, error) {
	var names []string
	for _, entry := range entries {
		if name := strings.TrimSpace(entry); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		names = appconfig.DefaultTrustedProxies
	}
	var networks []*net.IPNet
	for _, entry := range names {
		switch strings.ToLower(entry) {
		case "none":
			if len(names) != 1 {
				return nil, fmt.Errorf("trusted proxy none cannot be combined with other entries")
			}
			return nil, nil
		case "private":
			defaults, err := ParseTrustedProxies(appconfig.DefaultTrustedProxies)
			if err != nil {
				return nil, err
			}
			networks = append(networks, defaults...)
		default:
			if _, network, err := net.ParseCIDR(entry); err == nil {
				networks = append(networks, network)
				continue
			}
			address := net.ParseIP(entry)
			if address == nil {
				return nil, fmt.Errorf("invalid trusted proxy %q", entry)
			}
			bits := 128
			if address.To4() != nil {
				address, bits = address.To4(), 32
			}
			networks = append(networks, &net.IPNet{IP: address, Mask: net.CIDRMask(bits, bits)})
		}
	}
	return networks, nil
}

func clientIPFunc(trusted []*net.IPNet) app.ClientIP {
	return app.ClientIPWithOption(app.ClientIPOptions{
		RemoteIPHeaders: []string{"X-Forwarded-For", "X-Real-IP"}, TrustedCIDRs: trusted,
	})
}
