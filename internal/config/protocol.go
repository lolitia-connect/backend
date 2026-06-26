package config

type Protocol string

const (
	Shadowsocks Protocol = "shadowsocks"
	Trojan      Protocol = "trojan"
	Vmess       Protocol = "vmess"
	Vless       Protocol = "vless"
	Hysteria    Protocol = "hysteria"
	Tuic        Protocol = "tuic"
	AnyTLS      Protocol = "anytls"
	Socks       Protocol = "socks"
	Naive       Protocol = "naive"
	HTTP        Protocol = "http"
	Mieru       Protocol = "mieru"
)
