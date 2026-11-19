package payment

import (
	"fmt"
	"net/url"
	"strings"
)

func NotifyURL(domain, siteHost, platform, token string, gateway bool) (string, error) {
	base := strings.TrimSpace(domain)
	if base == "" {
		base = strings.TrimSpace(strings.Split(siteHost, "\n")[0])
	}
	if base != "" && !strings.Contains(base, "://") {
		base = "https://" + base
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("configure a valid payment domain or site host")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if gateway {
		parsed.Path += "/api"
	}
	parsed.Path += "/v1/notify/" + platform + "/" + token
	return parsed.String(), nil
}
