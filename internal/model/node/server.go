package node

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/pkg/errors"
)

type Server struct {
	Id      int64
	Name    string
	Country string
	City    string
	//Ratio          float32
	Address         string
	Sort            int
	Protocols       string
	LastReportedAt  *time.Time
	Longitude       string
	Latitude        string
	LongitudeCenter string
	LatitudeCenter  string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (*Server) TableName() string {
	return "servers"
}

// MarshalProtocols Marshal server protocols to json
func (m *Server) MarshalProtocols(list []Protocol) error {
	// key = "type:id", unique within (type, id) per server
	validate := make(map[string]bool)
	// track max ID per type for auto-increment
	typeMaxId := make(map[string]int)
	for _, protocol := range list {
		if protocol.Id != "" {
			if n, err := strconv.Atoi(protocol.Id); err == nil && n > typeMaxId[protocol.Type] {
				typeMaxId[protocol.Type] = n
			}
		}
	}
	for i, protocol := range list {
		if protocol.Type == "" {
			return errors.New("protocol type is required")
		}
		if list[i].Id == "" {
			typeMaxId[protocol.Type]++
			list[i].Id = strconv.Itoa(typeMaxId[protocol.Type])
		}
		key := protocol.Type + ":" + list[i].Id
		if _, exists := validate[key]; exists {
			return errors.New("duplicate protocol id: " + key)
		}
		validate[key] = true
	}
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	m.Protocols = string(data)
	return nil
}

// UnmarshalProtocols Unmarshal server protocols from json
func (m *Server) UnmarshalProtocols() ([]Protocol, error) {
	var list []Protocol
	if m.Protocols == "" {
		return list, nil
	}
	err := json.Unmarshal([]byte(m.Protocols), &list)
	if err != nil {
		return nil, err
	}
	return list, nil
}

type Protocol struct {
	Id                      string `json:"id"`   // Stable protocol instance id. Not tied to display name.
	Name                    string `json:"name"` // Optional display name.
	Type                    string `json:"type"`
	Port                    uint16 `json:"port"`
	Enable                  bool   `json:"enable"`
	Security                string `json:"security,omitempty"`
	SNI                     string `json:"sni,omitempty"`
	AllowInsecure           bool   `json:"allow_insecure,omitempty"`
	Fingerprint             string `json:"fingerprint,omitempty"`
	RealityServerAddr       string `json:"reality_server_addr,omitempty"`
	RealityServerPort       int    `json:"reality_server_port,omitempty"`
	RealityPrivateKey       string `json:"reality_private_key,omitempty"`
	RealityPublicKey        string `json:"reality_public_key,omitempty"`
	RealityShortId          string `json:"reality_short_id,omitempty"`
	Transport               string `json:"transport,omitempty"`
	Host                    string `json:"host,omitempty"`
	Path                    string `json:"path,omitempty"`
	ServiceName             string `json:"service_name,omitempty"`
	Cipher                  string `json:"cipher,omitempty"`
	ServerKey               string `json:"server_key,omitempty"`
	Flow                    string `json:"flow,omitempty"`
	UoT                     bool   `json:"uot,omitempty"`                   // UDP over TCP
	UoTVersion              int    `json:"uot_version,omitempty"`           // UoT version (1 or 2)
	AcceptProxyProtocol     bool   `json:"accept_proxy_protocol,omitempty"` // accept proxy protocol
	HopPorts                string `json:"hop_ports,omitempty"`
	HopInterval             int    `json:"hop_interval,omitempty"`
	ObfsPassword            string `json:"obfs_password,omitempty"`
	DisableSNI              bool   `json:"disable_sni,omitempty"`
	ReduceRtt               bool   `json:"reduce_rtt,omitempty"`
	UDPRelayMode            string `json:"udp_relay_mode,omitempty"`
	CongestionController    string `json:"congestion_controller,omitempty"`
	Multiplex               string `json:"multiplex,omitempty"`                 // mux, eg: off/low/medium/high
	PaddingScheme           string `json:"padding_scheme,omitempty"`            // padding scheme
	UpMbps                  int    `json:"up_mbps,omitempty"`                   // upload speed limit
	DownMbps                int    `json:"down_mbps,omitempty"`                 // download speed limit
	Obfs                    string `json:"obfs,omitempty"`                      // obfs, 'none', 'http', 'tls'
	ObfsHost                string `json:"obfs_host,omitempty"`                 // obfs host
	ObfsPath                string `json:"obfs_path,omitempty"`                 // obfs path
	XhttpMode               string `json:"xhttp_mode,omitempty"`                // xhttp mode
	XhttpExtra              string `json:"xhttp_extra,omitempty"`               // xhttp extra path
	Encryption              string `json:"encryption,omitempty"`                // encryption，'none', 'mlkem768x25519plus'
	EncryptionMode          string `json:"encryption_mode,omitempty"`           // encryption mode，'native', 'xorpub', 'random'
	EncryptionRtt           string `json:"encryption_rtt,omitempty"`            // encryption rtt，'0rtt', '1rtt'
	EncryptionTicket        string `json:"encryption_ticket,omitempty"`         // encryption ticket
	EncryptionServerPadding string `json:"encryption_server_padding,omitempty"` // encryption server padding
	EncryptionPrivateKey    string `json:"encryption_private_key,omitempty"`    // encryption private key
	EncryptionClientPadding string `json:"encryption_client_padding,omitempty"` // encryption client padding
	EncryptionPassword      string `json:"encryption_password,omitempty"`       // encryption password
	EchEnable               bool   `json:"ech_enable,omitempty"`                // ECH enable
	EchServerName           string `json:"ech_server_name,omitempty"`           // ECH SNI

	Ratio           float64 `json:"ratio,omitempty"`             // Traffic ratio, default is 1
	CertMode        string  `json:"cert_mode,omitempty"`         // Certificate mode, `none`｜`http`｜`dns`｜`self`
	CertDNSProvider string  `json:"cert_dns_provider,omitempty"` // DNS provider for certificate
	CertDNSEnv      string  `json:"cert_dns_env"`                // Environment for DNS provider
}

// Marshal protocol to json
func (m *Protocol) Marshal() ([]byte, error) {
	type Alias Protocol
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(m),
	})
}

// Unmarshal json to protocol
func (m *Protocol) Unmarshal(data []byte) error {
	type Alias Protocol
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(m),
	}
	return json.Unmarshal(data, &aux)
}
