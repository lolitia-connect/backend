package v2rayn

import (
	"github.com/perfect-panel/server/pkg/adapter/general"
	"github.com/perfect-panel/server/pkg/adapter/proxy"
)

type v2rayShareLink struct {
	Ps            string `json:"ps"`
	Add           string `json:"add"`
	Port          string `json:"port"`
	ID            string `json:"id"`
	Aid           string `json:"aid"`
	Net           string `json:"net"`
	Type          string `json:"type"`
	Host          string `json:"host"`
	SNI           string `json:"sni"`
	Path          string `json:"path"`
	TLS           string `json:"tls"`
	Flow          string `json:"flow,omitempty"`
	Alpn          string `json:"alpn,omitempty"`
	AllowInsecure bool   `json:"allowInsecure,omitempty"`
	Fingerprint   string `json:"fp,omitempty"`
	PublicKey     string `json:"pbk,omitempty"`
	ShortId       string `json:"sid,omitempty"`
	SpiderX       string `json:"spx,omitempty"`
	V             string `json:"v"`
}
type V2rayN struct {
	proxy.Adapter
}

func NewV2rayN(adapter proxy.Adapter) *V2rayN {
	return &V2rayN{
		Adapter: adapter,
	}
}
func (m *V2rayN) Build(uuid string) []byte {
	return general.GenerateBase64General(m.Adapter.Proxies, uuid)
}
